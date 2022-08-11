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

package applier

import (
	"fmt"
	"sort"
	"sync"
	"testing"

	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

func TestObjectStatusMapFilter(t *testing.T) {
	deploymentID := object.UnstructuredToObjMetadata(newDeploymentObj())
	testID := object.UnstructuredToObjMetadata(newTestObj())

	testcases := []struct {
		name      string
		input     ObjectStatusMap
		strategy  actuation.ActuationStrategy
		actuation actuation.ActuationStatus
		reconcile actuation.ReconcileStatus
		expected  []core.ID
	}{
		{
			name: "strategy delete",
			input: ObjectStatusMap{
				idFrom(deploymentID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileFailed,
				},
				idFrom(testID): {
					Strategy:  actuation.ActuationStrategyDelete,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileSucceeded,
				},
			},
			strategy:  actuation.ActuationStrategyDelete,
			actuation: -1,
			reconcile: -1,
			expected: []core.ID{
				idFrom(testID),
			},
		},
		{
			name: "apply succeeded",
			input: ObjectStatusMap{
				idFrom(deploymentID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileFailed,
				},
				idFrom(testID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationFailed,
					Reconcile: actuation.ReconcilePending,
				},
			},
			strategy:  actuation.ActuationStrategyApply,
			actuation: actuation.ActuationSucceeded,
			reconcile: -1,
			expected: []core.ID{
				idFrom(deploymentID),
			},
		},
		{
			name: "reconcile succeeded",
			input: ObjectStatusMap{
				idFrom(deploymentID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileFailed,
				},
				idFrom(testID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileSucceeded,
				},
			},
			strategy:  actuation.ActuationStrategyApply,
			actuation: actuation.ActuationSucceeded,
			reconcile: actuation.ReconcileSucceeded,
			expected: []core.ID{
				idFrom(testID),
			},
		},
		{
			name: "reconcile failed",
			input: ObjectStatusMap{
				idFrom(deploymentID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileFailed,
				},
				idFrom(testID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileSucceeded,
				},
			},
			strategy:  actuation.ActuationStrategyApply,
			actuation: actuation.ActuationSucceeded,
			reconcile: actuation.ReconcileFailed,
			expected: []core.ID{
				idFrom(deploymentID),
			},
		},
		{
			name: "reconcile any",
			input: ObjectStatusMap{
				idFrom(deploymentID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileFailed,
				},
				idFrom(testID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileSucceeded,
				},
			},
			strategy:  actuation.ActuationStrategyApply,
			actuation: actuation.ActuationSucceeded,
			reconcile: -1,
			expected: []core.ID{
				idFrom(deploymentID),
				idFrom(testID),
			},
		},
	}
	for _, tc := range testcases {
		result := tc.input.Filter(tc.strategy, tc.actuation, tc.reconcile)
		// Map range order is semi-random, so sort result to avoid flakey test failures.
		sort.Slice(result, func(i, j int) bool {
			return result[i].String() < result[j].String()
		})
		testutil.AssertEqual(t, tc.expected, result, "[%s] unexpected Filter return", tc.name)
	}
}

func TestObjectStatusMapLog(t *testing.T) {
	deploymentID := object.UnstructuredToObjMetadata(newDeploymentObj())
	testID := object.UnstructuredToObjMetadata(newTestObj())

	testcases := []struct {
		name     string
		input    ObjectStatusMap
		expected []string
	}{
		{
			name: "log status",
			input: ObjectStatusMap{
				idFrom(deploymentID): {
					Strategy:  actuation.ActuationStrategyApply,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileFailed,
				},
				idFrom(testID): {
					Strategy:  actuation.ActuationStrategyDelete,
					Actuation: actuation.ActuationSucceeded,
					Reconcile: actuation.ReconcileSucceeded,
				},
			},
			expected: []string{
				"Apply Actuations (Total: 2):\n" +
					"Skipped (0),\n" +
					"Succeeded (2): [apps/namespaces/test-namespace/Deployment/random-name, configsync.test/namespaces/test-namespace/Test/random-name],\n" +
					"Failed (0)",
				"Apply Reconciles (Total: 2):\n" +
					"Skipped (0),\n" +
					"Succeeded (1): [configsync.test/namespaces/test-namespace/Test/random-name],\n" +
					"Failed (1): [apps/namespaces/test-namespace/Deployment/random-name],\n" +
					"Timeout (0)",
				"Prune Actuations (Total: 1):\n" +
					"Skipped (0),\n" +
					"Succeeded (1): [configsync.test/namespaces/test-namespace/Test/random-name],\n" +
					"Failed (0)",
				"Prune Reconciles (Total: 1):\n" +
					"Skipped (0),\n" +
					"Succeeded (1): [configsync.test/namespaces/test-namespace/Test/random-name],\n" +
					"Failed (0),\n" +
					"Timeout (0)",
			},
		},
	}
	for _, tc := range testcases {
		logger := &fakeLogger{}
		logger.Enable()
		tc.input.Log(logger)
		testutil.AssertEqual(t, tc.expected, logger.Logs, "[%s] unexpected log messages", tc.name)
	}
}

type fakeLogger struct {
	lock    sync.Mutex
	enabled bool
	Logs    []string
}

func (fl *fakeLogger) Enable() {
	fl.enabled = true
}

func (fl *fakeLogger) Disable() {
	fl.lock.Lock()
	defer fl.lock.Unlock()
	fl.enabled = false
}

func (fl *fakeLogger) Enabled() bool {
	fl.lock.Lock()
	defer fl.lock.Unlock()
	return fl.enabled
}

func (fl *fakeLogger) Infof(format string, args ...interface{}) {
	fl.lock.Lock()
	defer fl.lock.Unlock()
	fl.Logs = append(fl.Logs, fmt.Sprintf(format, args...))
}
