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
		stats      disabledObjStats
		wantEmpty  bool
		wantString string
	}{
		{
			name:       "empty disabledObjStats",
			stats:      disabledObjStats{},
			wantEmpty:  true,
			wantString: "",
		},
		{
			name: "non-empty disabledObjStats",
			stats: disabledObjStats{
				total:     4,
				succeeded: 0,
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
				errCount:  0,
				eventByOp: map[event.PruneEventOperation]uint64{},
			},
			wantEmpty:  true,
			wantString: "",
		},
		{
			name: "non-empty pruneEventStats",
			stats: pruneEventStats{
				errCount: 1,
				eventByOp: map[event.PruneEventOperation]uint64{
					event.PruneSkipped: 4,
					event.Pruned:       0,
				},
			},
			wantEmpty:  false,
			wantString: "PruneEvent including an error: 1, PruneEvent events (OpType: PruneSkipped): 4",
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
				errCount:  0,
				eventByOp: map[event.ApplyEventOperation]uint64{},
			},
			wantEmpty:  true,
			wantString: "",
		},
		{
			name: "non-empty applyEventStats",
			stats: applyEventStats{
				errCount: 2,
				eventByOp: map[event.ApplyEventOperation]uint64{
					event.ServersideApplied: 4,
					event.Created:           2,
				},
			},
			wantEmpty:  false,
			wantString: "ApplyEvent including an error: 2, ApplyEvent events (OpType: ServersideApplied): 4, ApplyEvent events (OpType: Created): 2",
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
		stats      applyStats
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
			stats: applyStats{
				applyEvent: applyEventStats{
					eventByOp: map[event.ApplyEventOperation]uint64{
						event.Created:    1,
						event.Configured: 2,
					},
				},
				pruneEvent: pruneEventStats{
					errCount:  3,
					eventByOp: map[event.PruneEventOperation]uint64{},
				},
				errorTypeEvents: 4,
			},
			wantEmpty:  false,
			wantString: "ApplyEvent events (OpType: Created): 1, ApplyEvent events (OpType: Configured): 2, PruneEvent including an error: 3, ErrorType events: 4",
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
