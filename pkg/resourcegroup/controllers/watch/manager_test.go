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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func fakeRunnable(ctx context.Context) Runnable {
	cfg := watcherConfig{
		startWatch: func(options metav1.ListOptions) (watch.Interface, error) {
			return watch.NewFake(), nil
		},
	}
	return NewFiltered(ctx, cfg)
}

func fakeError(gvk schema.GroupVersionKind) error {
	return errors.Errorf("failed error for %q", gvk)
}

func testRunnables(_ context.Context, errOnType map[schema.GroupVersionKind]bool) func(context.Context, watcherConfig) (Runnable, error) {
	return func(ctx context.Context, cfg watcherConfig) (runnable Runnable, err error) {
		if errOnType[cfg.gvk] {
			return nil, fakeError(cfg.gvk)
		}
		return fakeRunnable(ctx), nil
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
				namespaceKind(): {},
				roleKind():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{namespaceKind(), roleKind()},
		},
		{
			name: "keep watchers if still declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				namespaceKind(): fakeRunnable(context.Background()),
				roleKind():      fakeRunnable(context.Background()),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				namespaceKind(): {},
				roleKind():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{namespaceKind(), roleKind()},
		},
		{
			name: "delete watchers if nothing declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				namespaceKind(): fakeRunnable(context.Background()),
				roleKind():      fakeRunnable(context.Background()),
			},
			gvks: map[schema.GroupVersionKind]struct{}{},
		},
		{
			name: "add/keep/delete watchers",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				roleKind():        fakeRunnable(context.Background()),
				roleBindingKind(): fakeRunnable(context.Background()),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				namespaceKind(): {},
				roleKind():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{namespaceKind(), roleKind()},
		},
		{
			name:       "error on starting watcher",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			failedWatchers: map[schema.GroupVersionKind]bool{
				roleKind(): true,
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				namespaceKind(): {},
				roleKind():      {},
			},
			wantWatchedTypes: []schema.GroupVersionKind{namespaceKind()},
			wantErr:          fakeError(roleKind()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			options := &Options{
				watcherFunc: testRunnables(ctx, tc.failedWatchers),
			}
			ch := make(chan event.GenericEvent)
			m, err := NewManager(nil, nil, ch, options)
			if err != nil {
				t.Fatal(err)
			}
			m.watcherMap = tc.watcherMap

			gotErr := m.UpdateWatches(context.Background(), tc.gvks)

			wantErr := tc.wantErr
			if wantErr != nil && wantErr.Error() != gotErr.Error() {
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

func namespaceKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Version: "v1",
		Kind:    "Namespace",
	}
}

func roleKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "Role",
	}
}

func roleBindingKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   "rbac.authorization.k8s.io",
		Version: "v1",
		Kind:    "RoleBinding",
	}
}
