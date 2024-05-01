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

package watch

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
)

func fakeRunnable() Runnable {
	cfg := watcherConfig{
		scope: "test",
		startWatch: func(_ context.Context, _ metav1.ListOptions) (watch.Interface, error) {
			return watch.NewFake(), nil
		},
		conflictHandler: fake.NewConflictHandler(),
	}
	return NewFiltered(cfg)
}

func fakeError(gvk schema.GroupVersionKind) status.Error {
	return status.APIServerErrorf(errors.New("failed"), "watcher failed for %s", gvk.String())
}

func testRunnables(errOnType map[schema.GroupVersionKind]bool) watcherFactory {
	return func(cfg watcherConfig) (runnable Runnable, err status.Error) {
		if errOnType[cfg.gvk] {
			return nil, fakeError(cfg.gvk)
		}
		return fakeRunnable(), nil
	}
}

func TestManager_Update(t *testing.T) {
	testCases := []struct {
		name string
		// watcherMap is the manager's map of watchers before the test begins.
		watcherMap map[schema.GroupVersionKind]Runnable
		// failedWatchers is the set of watchers which, if attempted to be
		// initialized, fail.
		failedWatchers map[schema.GroupVersionKind]bool
		// gvks is the map of gvks to update watches for. True/false indicates
		// validity to establish a new watch.
		gvks map[schema.GroupVersionKind]struct{}
		// wantWatchedTypes is the set of GVKS we want the Manager to be watching at
		// the end of the test.
		wantWatchedTypes []schema.GroupVersionKind
		// wantErr, if non-nil, reports that we want Update to return an error.
		wantErr error
	}{
		// Base Case.
		{
			name:           "no watchers and nothing declared",
			watcherMap:     map[schema.GroupVersionKind]Runnable{},
			failedWatchers: map[schema.GroupVersionKind]bool{},
			gvks:           map[schema.GroupVersionKind]struct{}{},
			wantErr:        nil,
		},
		// Watcher set mutations.
		{
			name:       "add watchers if declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{kinds.Namespace(), kinds.Role()},
		},
		{
			name: "keep watchers if still declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Namespace(): fakeRunnable(),
				kinds.Role():      fakeRunnable(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{kinds.Namespace(), kinds.Role()},
		},
		{
			name: "delete watchers if nothing declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Namespace(): fakeRunnable(),
				kinds.Role():      fakeRunnable(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{},
		},
		{
			name: "add/keep/delete watchers",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Role():        fakeRunnable(),
				kinds.RoleBinding(): fakeRunnable(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{kinds.Namespace(), kinds.Role()},
		},
		// Error case.
		{
			name:       "error on starting watcher",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			failedWatchers: map[schema.GroupVersionKind]bool{
				kinds.Role(): true,
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{kinds.Namespace()},
			wantErr:          fakeError(kinds.Role()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			options := &Options{
				watcherFactory: testRunnables(tc.failedWatchers),
			}
			m, err := NewManager(":test", "rs", nil, nil, &declared.Resources{}, options, fake.NewConflictHandler())
			if err != nil {
				t.Fatal(err)
			}
			m.watcherMap = tc.watcherMap

			gotErr := m.UpdateWatches(context.Background(), tc.gvks)

			if !errors.Is(tc.wantErr, gotErr) {
				t.Errorf("got UpdateWatches() error = %v, want %v", gotErr, tc.wantErr)
			}
			if diff := cmp.Diff(tc.wantWatchedTypes, m.watchedGVKs(), cmpopts.SortSlices(sortGVKs)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func sortGVKs(l, r schema.GroupVersionKind) bool {
	return l.String() < r.String()
}
