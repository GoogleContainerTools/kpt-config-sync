// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package reconcile

import (
	"context"
	"testing"
	"time"

	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/testing/testmetrics"
)

// durations creates a sequence of evenly-spaced time.Durations.
// The algorithm is least sensitive to evenly-spaced updates, so triggering
// on such patterns ensures it will trigger on other patterns.
func durations(diff time.Duration, n int) []time.Duration {
	result := make([]time.Duration, n)
	var d time.Duration
	for i := 0; i < n; i++ {
		result[i] = d
		d += diff
	}
	return result
}

var fourUpdatesAtOnce = durations(0, 4)
var sixUpdatesAtOnce = durations(0, 6)

func TestFight(t *testing.T) {
	testCases := []struct {
		name string
		// startHeat is the initial heat at the start of the simulation.
		// This is our initial estimate of updates per minute.
		startHeat float64
		// deltas are a monotonically nondecreasing sequence of update times,
		// as durations measured from the beginning of the simulation.
		deltas []time.Duration
		// whether we want the estimated updates per minute at the end of the
		// test to be above or equal to our threshold.
		wantAboveThreshold bool
	}{
		// Sets of immediate updates.
		{
			name:               "one is below threshold",
			deltas:             durations(0, 1),
			wantAboveThreshold: false,
		},
		{
			name:               "four at once is below threshold",
			deltas:             durations(0, 4),
			wantAboveThreshold: false,
		},
		{
			name:               "six at once is at threshold",
			deltas:             sixUpdatesAtOnce,
			wantAboveThreshold: true,
		},
		// Evenly spread updates takes more time to adjust to.
		{
			name:               "seven over a minute is below threshold",
			deltas:             durations(time.Minute/6.0, 7),
			wantAboveThreshold: false,
		},
		{
			name: "eight over a minute is above threshold",
			// This is sufficient to prove that *any* pattern of eight updates in 1 minute
			// will exceed the heat threshold.
			deltas:             durations(time.Minute/7.0, 8),
			wantAboveThreshold: true,
		},
		// Five updates per minute eventually triggers threshold, but not immediately.
		{
			name:               "five per minute over two minutes is below threshold",
			deltas:             durations(time.Minute/5.0, 10),
			wantAboveThreshold: false,
		},
		{
			name: "five per minute over three minutes is above threshold",
			// As above, this proves that *any* pattern of sixteen updates in 3 minutes
			// will exceed the heat threshold.
			deltas:             durations(time.Minute/5.0, 16),
			wantAboveThreshold: true,
		},
		{
			name:               "starting from high heat does not immediately adjust to lower frequency",
			startHeat:          60.0,
			deltas:             durations(time.Minute/200.0, 4),
			wantAboveThreshold: true,
		},
		{
			name:               "high heat eventually adjusts to new lower frequency",
			startHeat:          60.0,
			deltas:             durations(time.Minute/2.0, 8),
			wantAboveThreshold: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			f := fight{
				heat: tc.startHeat,
				last: now,
			}

			heat := tc.startHeat
			for _, d := range tc.deltas {
				heat = f.markUpdated(now.Add(d))
			}

			if heat < fightThreshold && tc.wantAboveThreshold {
				t.Errorf("got heat = %f < %f, want heat >= %f", heat, fightThreshold, fightThreshold)
			} else if heat >= fightThreshold && !tc.wantAboveThreshold {
				t.Errorf("got heat = %f >= %f, want heat < %f", heat, fightThreshold, fightThreshold)
			}
		})
	}
}

func TestFightDetector(t *testing.T) {
	roleGK := kinds.Role().GroupKind()
	roleBindingGK := kinds.RoleBinding().GroupKind()
	testCases := []struct {
		name               string
		updates            map[gknn][]time.Duration
		wantAboveThreshold map[gknn]bool
	}{
		{
			name: "one object below threshold",
			updates: map[gknn][]time.Duration{
				{gk: roleGK, namespace: "foo", name: "admin"}: fourUpdatesAtOnce,
			},
		},
		{
			name: "one object above threshold",
			updates: map[gknn][]time.Duration{
				{gk: roleGK, namespace: "foo", name: "admin"}: sixUpdatesAtOnce,
			},
			wantAboveThreshold: map[gknn]bool{
				{gk: roleGK, namespace: "foo", name: "admin"}: true,
			},
		},
		{
			name: "four objects objects below threshold",
			updates: map[gknn][]time.Duration{
				{gk: roleGK, namespace: "foo", name: "admin"}:        fourUpdatesAtOnce,
				{gk: roleBindingGK, namespace: "foo", name: "admin"}: fourUpdatesAtOnce,
				{gk: roleGK, namespace: "bar", name: "admin"}:        fourUpdatesAtOnce,
				{gk: roleGK, namespace: "foo", name: "user"}:         fourUpdatesAtOnce,
			},
		},
		{
			name: "two of four objects objects above threshold",
			updates: map[gknn][]time.Duration{
				{gk: roleGK, namespace: "foo", name: "admin"}:        sixUpdatesAtOnce,
				{gk: roleBindingGK, namespace: "foo", name: "admin"}: fourUpdatesAtOnce,
				{gk: roleGK, namespace: "bar", name: "admin"}:        fourUpdatesAtOnce,
				{gk: roleGK, namespace: "foo", name: "user"}:         sixUpdatesAtOnce,
			},
			wantAboveThreshold: map[gknn]bool{
				{gk: roleGK, namespace: "foo", name: "admin"}: true,
				{gk: roleGK, namespace: "foo", name: "user"}:  true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fd := newFightDetector()

			now := time.Now()
			for o, updates := range tc.updates {
				u := fake.Unstructured(o.gk.WithVersion(""), core.Namespace(o.namespace), core.Name(o.name))

				aboveThreshold := false
				for _, update := range updates {
					fight := fd.markUpdated(now.Add(update), u)
					aboveThreshold = aboveThreshold || fight != nil
				}
				if tc.wantAboveThreshold[o] && !aboveThreshold {
					t.Errorf("got markUpdated(%v) = false, want true", o)
				} else if !tc.wantAboveThreshold[o] && aboveThreshold {
					t.Errorf("got markUpdated(%v) = true, want false", o)
				}
			}
		})
	}
}

func TestResourceFightsMetricValidation(t *testing.T) {
	roleGVK := kinds.Role().GroupKind().WithVersion("")
	roleBindingGVK := kinds.RoleBinding().GroupKind().WithVersion("")
	fl := newFightLogger()
	testCases := []struct {
		name           string
		fightThreshold float64
		operations     []string
		gvk            schema.GroupVersionKind
		wantMetrics    []*view.Row
	}{
		{
			name:           "fight detected while creating Role",
			fightThreshold: 0, // Setting to 0 to guarantee a fight is detected
			operations:     []string{"create"},
			gvk:            roleGVK,
			wantMetrics: []*view.Row{
				{Data: &view.CountData{Value: 1}, Tags: []tag.Tag{
					{Key: metrics.KeyOperation, Value: "create"},
					{Key: metrics.KeyType, Value: "Role"}}},
			},
		},
		{
			name:           "multiple fights detected while deleting RoleBinding",
			fightThreshold: 0,
			operations:     []string{"delete", "delete"},
			gvk:            roleBindingGVK,
			wantMetrics: []*view.Row{
				{Data: &view.CountData{Value: 2}, Tags: []tag.Tag{
					{Key: metrics.KeyOperation, Value: "delete"},
					{Key: metrics.KeyType, Value: "RoleBinding"}}},
			},
		},
		{
			name:           "no fights detected while updating Role",
			fightThreshold: 5.0,
			operations:     []string{"update"},
			gvk:            roleGVK,
			wantMetrics:    []*view.Row{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			m := testmetrics.RegisterMetrics(metrics.ResourceFightsView)
			fd := newFightDetector()
			SetFightThreshold(tc.fightThreshold)
			u := fake.UnstructuredObject(tc.gvk)

			for _, op := range tc.operations {
				fd.detectFight(context.Background(), time.Now(), u, &fl, op)
			}

			if diff := m.ValidateMetrics(metrics.ResourceFightsView, tc.wantMetrics); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}
