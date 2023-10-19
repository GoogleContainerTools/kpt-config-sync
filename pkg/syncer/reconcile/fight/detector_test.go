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

package fight

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
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
		// update indicates whether this function is invoked with an update or a simple cooldown check.
		update bool
		// whether we want the estimated updates per minute at the end of the
		// test to be above or equal to our threshold.
		wantAboveThreshold bool
	}{
		// Sets of immediate updates.
		{
			name:               "one is below threshold",
			deltas:             durations(0, 1),
			update:             true,
			wantAboveThreshold: false,
		},
		{
			name:               "four at once is below threshold",
			deltas:             durations(0, 4),
			update:             true,
			wantAboveThreshold: false,
		},
		{
			name:               "six at once is at threshold",
			deltas:             sixUpdatesAtOnce,
			update:             true,
			wantAboveThreshold: true,
		},
		// Evenly spread updates takes more time to adjust to.
		{
			name:               "seven over a minute is below threshold",
			deltas:             durations(time.Minute/6.0, 7),
			update:             true,
			wantAboveThreshold: false,
		},
		{
			name: "eight over a minute is above threshold",
			// This is sufficient to prove that *any* pattern of eight updates in 1 minute
			// will exceed the heat threshold.
			deltas:             durations(time.Minute/7.0, 8),
			update:             true,
			wantAboveThreshold: true,
		},
		// Five updates per minute eventually triggers threshold, but not immediately.
		{
			name:               "five per minute over two minutes is below threshold",
			deltas:             durations(time.Minute/5.0, 10),
			update:             true,
			wantAboveThreshold: false,
		},
		{
			name: "five per minute over three minutes is above threshold",
			// As above, this proves that *any* pattern of sixteen updates in 3 minutes
			// will exceed the heat threshold.
			deltas:             durations(time.Minute/5.0, 16),
			update:             true,
			wantAboveThreshold: true,
		},
		{
			name:               "starting from high heat does not immediately adjust to lower frequency",
			startHeat:          60.0,
			deltas:             durations(time.Minute/200.0, 4),
			update:             true,
			wantAboveThreshold: true,
		},
		{
			name:               "high heat eventually adjusts to new lower frequency",
			startHeat:          60.0,
			deltas:             durations(time.Minute/2.0, 8),
			update:             true,
			wantAboveThreshold: false,
		},
		// Heat decays much faster when no updates.
		{
			name:               "high heat may never decay with update",
			startHeat:          60.0,
			deltas:             durations(time.Minute/6.0, 50),
			update:             true,
			wantAboveThreshold: true,
		}, {
			name:               "high heat decays much faster without updates",
			startHeat:          60.0,
			deltas:             durations(time.Minute/6.0, 6),
			update:             false,
			wantAboveThreshold: true,
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
				heat = f.refreshUpdateFrequency(now.Add(d), tc.update)
			}

			if heat < fightThreshold && tc.wantAboveThreshold {
				t.Errorf("got heat = %f < %f, want heat >= %f", heat, fightThreshold, fightThreshold)
			} else if heat >= fightThreshold && !tc.wantAboveThreshold {
				t.Errorf("got heat = %f >= %f, want heat < %f", heat, fightThreshold, fightThreshold)
			}
		})
	}
}

func roleID(name, ns string) core.ID {
	return core.IDOf(fake.RoleObject(core.Name(name), core.Namespace(ns)))
}

func roleBindingID(name, ns string) core.ID {
	return core.IDOf(fake.RoleBinding(core.Name(name), core.Namespace(ns)))
}

func TestFightDetector(t *testing.T) {
	testCases := []struct {
		name               string
		updates            map[core.ID][]time.Duration
		wantAboveThreshold map[core.ID]bool
	}{
		{
			name: "one object below threshold",
			updates: map[core.ID][]time.Duration{
				roleID("admin", "foo"): fourUpdatesAtOnce,
			},
		},
		{
			name: "one object above threshold",
			updates: map[core.ID][]time.Duration{
				roleID("admin", "foo"): sixUpdatesAtOnce,
			},
			wantAboveThreshold: map[core.ID]bool{
				roleID("admin", "foo"): true,
			},
		},
		{
			name: "four objects objects below threshold",
			updates: map[core.ID][]time.Duration{
				roleID("admin", "foo"):        fourUpdatesAtOnce,
				roleBindingID("admin", "foo"): fourUpdatesAtOnce,
				roleID("admin", "bar"):        fourUpdatesAtOnce,
				roleID("user", "foo"):         fourUpdatesAtOnce,
			},
		},
		{
			name: "two of four objects objects above threshold",
			updates: map[core.ID][]time.Duration{
				roleID("admin", "foo"):        sixUpdatesAtOnce,
				roleBindingID("admin", "foo"): fourUpdatesAtOnce,
				roleID("admin", "bar"):        fourUpdatesAtOnce,
				roleID("user", "foo"):         sixUpdatesAtOnce,
			},
			wantAboveThreshold: map[core.ID]bool{
				roleID("admin", "foo"): true,
				roleID("user", "foo"):  true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fd := NewDetector()

			now := time.Now()
			for id, updates := range tc.updates {
				u := fake.Unstructured(id.WithVersion(""), core.Namespace(id.Namespace), core.Name(id.Name))

				aboveThreshold := false
				logged := false
				for i, update := range updates {
					logErr, fightErr := fd.DetectFight(now.Add(update), u)
					if i+1 >= int(fightThreshold) {
						require.Error(t, fightErr)
						aboveThreshold = true
						if logged {
							require.False(t, logErr)
						} else {
							require.True(t, logErr)
							logged = true
						}
					} else {
						require.NoError(t, fightErr)
					}
				}
				if tc.wantAboveThreshold[id] && !aboveThreshold {
					t.Errorf("got refreshUpdateFrequency(%v) = false, want true", id)
				} else if !tc.wantAboveThreshold[id] && aboveThreshold {
					t.Errorf("got refreshUpdateFrequency(%v) = true, want false", id)
				}
			}
		})
	}
}
