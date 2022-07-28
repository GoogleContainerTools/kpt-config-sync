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
	"testing"

	"sigs.k8s.io/cli-utils/pkg/apply/event"
)

func TestDisabledObjStats(t *testing.T) {
	testcases := []struct {
		name       string
		stats      DisabledObjStats
		wantEmpty  bool
		wantString string
	}{
		{
			name:       "empty disabledObjStats",
			stats:      DisabledObjStats{},
			wantEmpty:  true,
			wantString: "",
		},
		{
			name: "non-empty disabledObjStats",
			stats: DisabledObjStats{
				Total:     4,
				Succeeded: 0,
			},
			wantEmpty:  false,
			wantString: "disabled 0 out of 4 objects",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.stats.empty() != tc.wantEmpty {
				t.Errorf("stats.empty() = %t, wanted %t", tc.stats.empty(), tc.wantEmpty)
			}

			if tc.stats.string() != tc.wantString {
				t.Errorf("stats.string() = %q, wanted %q", tc.stats.string(), tc.wantString)
			}

		})
	}
}

func TestPruneEventStats(t *testing.T) {
	testcases := []struct {
		name       string
		stats      pruneEventStats
		wantEmpty  bool
		wantString string
	}{
		{
			name: "empty pruneEventStats",
			stats: pruneEventStats{
				EventByOp: map[event.PruneEventStatus]uint64{},
			},
			wantEmpty:  true,
			wantString: "",
		},
		{
			name: "non-empty pruneEventStats",
			stats: pruneEventStats{
				EventByOp: map[event.PruneEventStatus]uint64{
					event.PruneSkipped:    4,
					event.PruneSuccessful: 0,
					event.PruneFailed:     1,
				},
			},
			wantEmpty:  false,
			wantString: "PruneEvents: 5 (Skipped: 4, Failed: 1)",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.stats.empty() != tc.wantEmpty {
				t.Errorf("stats.empty() = %t, wanted %t", tc.stats.empty(), tc.wantEmpty)
			}

			if tc.stats.string() != tc.wantString {
				t.Errorf("stats.string() = %q, wanted %q", tc.stats.string(), tc.wantString)
			}

		})
	}
}

func TestApplyEventStats(t *testing.T) {
	testcases := []struct {
		name       string
		stats      applyEventStats
		wantEmpty  bool
		wantString string
	}{
		{
			name: "empty applyEventStats",
			stats: applyEventStats{
				EventByOp: map[event.ApplyEventStatus]uint64{},
			},
			wantEmpty:  true,
			wantString: "",
		},
		{
			name: "non-empty applyEventStats",
			stats: applyEventStats{
				EventByOp: map[event.ApplyEventStatus]uint64{
					event.ApplySuccessful: 4,
					event.ApplySkipped:    2,
					event.ApplyFailed:     2,
				},
			},
			wantEmpty:  false,
			wantString: "ApplyEvents: 8 (Successful: 4, Skipped: 2, Failed: 2)",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.stats.empty() != tc.wantEmpty {
				t.Errorf("stats.empty() = %t, wanted %t", tc.stats.empty(), tc.wantEmpty)
			}

			if tc.stats.string() != tc.wantString {
				t.Errorf("stats.string() = %q, wanted %q", tc.stats.string(), tc.wantString)
			}

		})
	}
}

func TestApplyStats(t *testing.T) {
	testcases := []struct {
		name       string
		stats      ApplyStats
		wantEmpty  bool
		wantString string
	}{
		{
			name:       "empty applyStats",
			stats:      newApplyStats(),
			wantEmpty:  true,
			wantString: "",
		},
		{
			name: "non-empty applyStats",
			stats: ApplyStats{
				ApplyEvent: applyEventStats{
					EventByOp: map[event.ApplyEventStatus]uint64{
						event.ApplySuccessful: 1,
						event.ApplySkipped:    2,
					},
				},
				PruneEvent: pruneEventStats{
					EventByOp: map[event.PruneEventStatus]uint64{
						event.PruneFailed: 3,
					},
				},
				ErrorTypeEvents: 4,
			},
			wantEmpty:  false,
			wantString: "ApplyEvents: 3 (Successful: 1, Skipped: 2), PruneEvents: 3 (Failed: 3), ErrorEvents: 4",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.stats.empty() != tc.wantEmpty {
				t.Errorf("stats.empty() = %t, wanted %t", tc.stats.empty(), tc.wantEmpty)
			}

			if tc.stats.string() != tc.wantString {
				t.Errorf("stats.string() = %q, wanted %q", tc.stats.string(), tc.wantString)
			}

		})
	}
}
