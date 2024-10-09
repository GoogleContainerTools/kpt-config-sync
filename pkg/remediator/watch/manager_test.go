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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/testerrors"
	utilwatch "kpt.dev/configsync/pkg/util/watch"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

func fakeRunnable(commit string) Runnable {
	cfg := watcherConfig{
		scope: "test",
		startWatch: func(_ context.Context, _ metav1.ListOptions) (watch.Interface, error) {
			return watch.NewFake(), nil
		},
		conflictHandler: fake.NewConflictHandler(),
		labelSelector:   labels.Everything(),
		commit:          commit,
	}
	return NewFiltered(cfg)
}

func fakeError(gvk schema.GroupVersionKind) status.Error {
	return status.APIServerErrorf(errors.New("failed"), "watcher failed for %s", gvk.String())
}

func testRunnables(errOnType map[schema.GroupVersionKind]bool, commit string) WatcherFactory {
	return func(cfg watcherConfig) (runnable Runnable, err status.Error) {
		if errOnType[cfg.gvk] {
			return nil, fakeError(cfg.gvk)
		}
		return fakeRunnable(commit), nil
	}
}

type GroupVersionKindWithCommit struct {
	Commit string
	GVK    schema.GroupVersionKind
}

// watchedGVKs returns a list of all GroupVersionKinds currently being watched.
func (m *Manager) watchedGVKs() []GroupVersionKindWithCommit {
	var gvks []GroupVersionKindWithCommit
	for gvk, watcher := range m.watcherMap {
		fw := watcher.(*filteredWatcher)
		gvks = append(gvks, GroupVersionKindWithCommit{
			GVK:    gvk,
			Commit: fw.getLatestCommit(),
		})
	}
	return gvks
}

func TestManager_AddWatches(t *testing.T) {
	previousCommit := "previous"
	currentCommit := "current"
	testCases := []struct {
		name string
		// watcherMap is the manager's map of watchers before the test begins.
		watcherMap map[schema.GroupVersionKind]Runnable
		// failedWatchers is the set of watchers which, if attempted to be
		// initialized, fail.
		failedWatchers map[schema.GroupVersionKind]bool
		// mappedGVKs is the list of gvks that the RESTMapper knows about
		mappedGVKs []schema.GroupVersionKind
		// gvks is the map of gvks to update watches for.
		gvks map[schema.GroupVersionKind]struct{}
		// wantWatchedTypes is the set of GVKS we want the Manager to be watching at
		// the end of the test.
		wantWatchedTypes []GroupVersionKindWithCommit
		// wantErr, if non-nil, reports that we want Update to return an error.
		wantErr status.MultiError
	}{
		// Base Case.
		{
			name:           "no watchers and nothing declared",
			watcherMap:     map[schema.GroupVersionKind]Runnable{},
			failedWatchers: map[schema.GroupVersionKind]bool{},
			mappedGVKs:     []schema.GroupVersionKind{},
			gvks:           map[schema.GroupVersionKind]struct{}{},
			wantErr:        nil,
		},
		// Watcher set mutations.
		{
			name:       "add watchers if declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
			},
		},
		{
			name: "keep watchers if still declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Namespace(): fakeRunnable(currentCommit),
				kinds.Role():      fakeRunnable(currentCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
			},
		},
		{
			name: "do not delete watchers if nothing declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Namespace(): fakeRunnable(currentCommit),
				kinds.Role():      fakeRunnable(currentCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{},
			gvks:       map[schema.GroupVersionKind]struct{}{},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
			},
		},
		{
			name: "add/keep/no-delete watchers",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Role():        fakeRunnable(currentCommit),
				kinds.RoleBinding(): fakeRunnable(currentCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
				kinds.RoleBinding(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
				{GVK: kinds.RoleBinding(), Commit: currentCommit},
			},
		},
		{
			name:       "error on starting watcher",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			failedWatchers: map[schema.GroupVersionKind]bool{
				kinds.Role(): true,
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
			},
			wantErr: fakeError(kinds.Role()),
		},
		{
			name:       "skip adding unknown resource",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
			},
		},
		{
			name: "update latest Commit for already started watchers",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Role():        fakeRunnable(previousCommit),
				kinds.RoleBinding(): fakeRunnable(previousCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
				kinds.RoleBinding(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
				{GVK: kinds.RoleBinding(), Commit: previousCommit},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			watcherFactory := testRunnables(tc.failedWatchers, currentCommit)
			fakeMapper := testutil.NewFakeRESTMapper(tc.mappedGVKs...)
			newMapperFn := func() (meta.RESTMapper, error) {
				// AddWatches doesn't call Reset. So we don't need to impl it.
				return fakeMapper, nil
			}
			mapper := utilwatch.NewReplaceOnResetRESTMapper(fakeMapper, newMapperFn)
			m, err := NewManager(":test", "rs", nil, &declared.Resources{},
				watcherFactory, mapper, fake.NewConflictHandler(),
				&controllers.CRDController{})
			if err != nil {
				t.Fatal(err)
			}
			m.watcherMap = tc.watcherMap

			err = m.AddWatches(context.Background(), tc.gvks, currentCommit)
			testerrors.AssertEqual(t, tc.wantErr, err)

			if diff := cmp.Diff(tc.wantWatchedTypes, m.watchedGVKs(), cmpopts.SortSlices(sortGVKs)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestManager_UpdateWatches(t *testing.T) {
	previousCommit := "previous"
	currentCommit := "current"
	testCases := []struct {
		name string
		// watcherMap is the manager's map of watchers before the test begins.
		watcherMap map[schema.GroupVersionKind]Runnable
		// failedWatchers is the set of watchers which, if attempted to be
		// initialized, fail.
		failedWatchers map[schema.GroupVersionKind]bool
		// mappedGVKs is the list of gvks that the RESTMapper knows about
		mappedGVKs []schema.GroupVersionKind
		// gvks is the map of gvks to update watches for.
		gvks map[schema.GroupVersionKind]struct{}
		// wantWatchedTypes is the set of GVKS we want the Manager to be watching at
		// the end of the test.
		wantWatchedTypes []GroupVersionKindWithCommit
		// wantErr, if non-nil, reports that we want Update to return an error.
		wantErr status.MultiError
	}{
		// Base Case.
		{
			name:           "no watchers and nothing declared",
			watcherMap:     map[schema.GroupVersionKind]Runnable{},
			failedWatchers: map[schema.GroupVersionKind]bool{},
			mappedGVKs:     []schema.GroupVersionKind{},
			gvks:           map[schema.GroupVersionKind]struct{}{},
			wantErr:        nil,
		},
		// Watcher set mutations.
		{
			name:       "add watchers if declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
			},
		},
		{
			name: "keep watchers if still declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Namespace(): fakeRunnable(currentCommit),
				kinds.Role():      fakeRunnable(currentCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
			},
		},
		{
			name: "delete watchers if nothing declared",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Namespace(): fakeRunnable(currentCommit),
				kinds.Role():      fakeRunnable(currentCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{},
			gvks:       map[schema.GroupVersionKind]struct{}{},
		},
		{
			name: "add/keep/delete watchers",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Role():        fakeRunnable(currentCommit),
				kinds.RoleBinding(): fakeRunnable(currentCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
			},
		},
		{
			name:       "error on starting watcher",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			failedWatchers: map[schema.GroupVersionKind]bool{
				kinds.Role(): true,
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
			},
			wantErr: fakeError(kinds.Role()),
		},
		{
			name:       "error adding unknown resource",
			watcherMap: map[schema.GroupVersionKind]Runnable{},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
			},
			wantErr: syncerclient.ConflictWatchResourceDoesNotExist(
				&meta.NoKindMatchError{
					GroupKind: kinds.Role().GroupKind(),
					SearchedVersions: []string{
						kinds.Role().Version,
					},
				}, kinds.Role()),
		},
		{
			name: "update latest Commit for already started watchers",
			watcherMap: map[schema.GroupVersionKind]Runnable{
				kinds.Role():        fakeRunnable(previousCommit),
				kinds.RoleBinding(): fakeRunnable(previousCommit),
			},
			mappedGVKs: []schema.GroupVersionKind{
				kinds.Namespace(),
				kinds.Role(),
			},
			gvks: map[schema.GroupVersionKind]struct{}{
				kinds.Namespace(): {},
				kinds.Role():      {},
			},
			wantWatchedTypes: []GroupVersionKindWithCommit{
				{GVK: kinds.Namespace(), Commit: currentCommit},
				{GVK: kinds.Role(), Commit: currentCommit},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			watcherFactory := testRunnables(tc.failedWatchers, currentCommit)
			fakeMapper := testutil.NewFakeRESTMapper(tc.mappedGVKs...)
			newMapperFn := func() (meta.RESTMapper, error) {
				// UpdateWatches doesn't call Reset. So we don't need to impl it.
				return fakeMapper, nil
			}
			mapper := utilwatch.NewReplaceOnResetRESTMapper(fakeMapper, newMapperFn)
			m, err := NewManager(":test", "rs", nil, &declared.Resources{},
				watcherFactory, mapper, fake.NewConflictHandler(),
				&controllers.CRDController{})
			if err != nil {
				t.Fatal(err)
			}
			m.watcherMap = tc.watcherMap

			err = m.UpdateWatches(context.Background(), tc.gvks, currentCommit)
			testerrors.AssertEqual(t, tc.wantErr, err)

			if diff := cmp.Diff(tc.wantWatchedTypes, m.watchedGVKs(), cmpopts.SortSlices(sortGVKs)); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func sortGVKs(l, r GroupVersionKindWithCommit) bool {
	return l.GVK.String() < r.GVK.String()
}
