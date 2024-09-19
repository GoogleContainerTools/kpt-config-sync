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

package nomostest

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

type watchForAllSyncsOptions struct {
	timeout            time.Duration
	readyCheck         bool
	syncRootRepos      bool
	syncNamespaceRepos bool
	skipRootRepos      map[string]bool
	skipNonRootRepos   map[types.NamespacedName]bool
	predicates         []testpredicates.Predicate
}

// WatchForAllSyncsOptions is an optional parameter for WaitForRepoSyncs.
type WatchForAllSyncsOptions func(*watchForAllSyncsOptions)

// WithTimeout provides the timeout to WaitForRepoSyncs.
func WithTimeout(timeout time.Duration) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.timeout = timeout
	}
}

// WithPredicates adds additional predicates for all reconcilers to WaitForRepoSyncs.
func WithPredicates(predicates ...testpredicates.Predicate) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.predicates = append(predicates, predicates...)
	}
}

// Sha1Func is the function type that retrieves the commit sha1.
type Sha1Func func(nt *NT, nn types.NamespacedName) (string, error)

// RootSyncOnly specifies that only the RootRepos should be synced.
func RootSyncOnly() WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.syncNamespaceRepos = false
	}
}

// RepoSyncOnly specifies that only the NonRootRepos should be synced.
func RepoSyncOnly() WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.syncRootRepos = false
	}
}

// SkipRootRepos specifies RootSyncs which do not need to be synced.
func SkipRootRepos(skipRootRepos ...string) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.skipRootRepos = make(map[string]bool)
		for _, skip := range skipRootRepos {
			options.skipRootRepos[skip] = true
		}
	}
}

// SkipNonRootRepos specifies RepoSyncs which do not need to be synced.
func SkipNonRootRepos(skipNonRootRepos ...types.NamespacedName) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.skipNonRootRepos = make(map[types.NamespacedName]bool)
		for _, skip := range skipNonRootRepos {
			options.skipNonRootRepos[skip] = true
		}
	}
}

// SkipReadyCheck specifies not to wait for all Config Sync components to be ready.
func SkipReadyCheck() WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.readyCheck = false
	}
}

// SyncPathPredicatePair is a pair of the sync directory and the predicate.
type SyncPathPredicatePair struct {
	Path      string
	Predicate func(string) testpredicates.Predicate
}

// WatchForAllSyncs calls WatchForSync on all Syncs in nt.SyncSources.
//
// If you want to validate specific fields of a Sync object, use
// nt.Watcher.WatchObject() instead.
func (nt *NT) WatchForAllSyncs(options ...WatchForAllSyncsOptions) error {
	nt.T.Helper()
	opts := watchForAllSyncsOptions{
		timeout:            nt.DefaultWaitTimeout,
		readyCheck:         true,
		syncRootRepos:      true,
		syncNamespaceRepos: true,
		skipRootRepos:      make(map[string]bool),
		skipNonRootRepos:   make(map[types.NamespacedName]bool),
		predicates:         []testpredicates.Predicate{},
	}
	// Override defaults with specified options
	for _, option := range options {
		option(&opts)
	}

	if opts.readyCheck {
		if err := WaitForConfigSyncReady(nt); err != nil {
			return err
		}
	}

	watchOptions := []testwatcher.WatchOption{
		testwatcher.WatchTimeout(opts.timeout),
	}
	if len(opts.predicates) > 0 {
		watchOptions = append(watchOptions, testwatcher.WatchPredicates(opts.predicates...))
	}

	tg := taskgroup.New()

	if opts.syncRootRepos {
		for id, source := range nt.SyncSources.RootSyncs() {
			if _, ok := opts.skipRootRepos[id.Name]; ok {
				continue
			}
			idPtr := id
			tg.Go(func() error {
				return nt.WatchForSync(kinds.RootSyncV1Beta1(), idPtr.Name, idPtr.Namespace,
					DefaultRootSha1Fn, RootSyncHasStatusSyncCommit,
					&SyncPathPredicatePair{Path: source.Path(), Predicate: RootSyncHasStatusSyncPath},
					watchOptions...)
			})
		}
	}

	if opts.syncNamespaceRepos {
		for id, source := range nt.SyncSources.RepoSyncs() {
			if _, ok := opts.skipNonRootRepos[id.ObjectKey]; ok {
				continue
			}
			idPtr := id
			tg.Go(func() error {
				return nt.WatchForSync(kinds.RepoSyncV1Beta1(), idPtr.Name, idPtr.Namespace,
					DefaultRepoSha1Fn, RepoSyncHasStatusSyncCommit,
					&SyncPathPredicatePair{Path: source.Path(), Predicate: RepoSyncHasStatusSyncPath},
					watchOptions...)
			})
		}
	}

	return tg.Wait()
}

// WatchForSync watches the specified sync object until it's synced.
//
//   - gvk (required) is the sync object GroupVersionKind
//   - name (required) is the sync object name
//   - namespace (required) is the sync object namespace
//   - sha1Func (required) is the function that to compute the expected commit.
//   - syncSha1 (required) is a Predicate factory used to validate whether the
//     object is synced, given the expected commit.
//   - syncDirPair (optional) is a pair of sync dir and the corresponding
//     predicate to validate the config directory.
//   - opts (optional) allows configuring the watcher (e.g. timeout)
func (nt *NT) WatchForSync(
	gvk schema.GroupVersionKind,
	name, namespace string,
	sha1Func Sha1Func,
	syncSha1 func(string) testpredicates.Predicate,
	syncDirPair *SyncPathPredicatePair,
	opts ...testwatcher.WatchOption,
) error {
	nt.T.Helper()
	if namespace == "" {
		// If namespace is empty, use the default namespace
		namespace = configsync.ControllerNamespace
	}
	nn := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	sha1, err := sha1Func(nt, nn)
	if err != nil {
		return fmt.Errorf("failed to retrieve sha1: %w", err)
	}

	predicates := []testpredicates.Predicate{
		// Wait until status.observedGeneration matches metadata.generation
		testpredicates.HasObservedLatestGeneration(nt.Scheme),
		// Wait until metadata.deletionTimestamp is missing, and conditions do not iniclude Reconciling=True or Stalled=True
		testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
		// Wait until expected commit/version is parsed, rendered, and synced
		syncSha1(sha1),
	}
	if syncDirPair != nil {
		predicates = append(predicates, syncDirPair.Predicate(syncDirPair.Path))
	}
	opts = append(opts, testwatcher.WatchPredicates(predicates...))

	err = nt.Watcher.WatchObject(gvk, name, namespace, opts...)
	if err != nil {
		return fmt.Errorf("waiting for sync: %w", err)
	}
	nt.T.Logf("%s %s/%s is synced", gvk.Kind, namespace, name)
	return nil
}

// WaitForRootSyncSourceError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSourceError(rsName, code string, message string, opts ...WaitOption) {
	nt.T.Helper()
	Wait(nt.T, fmt.Sprintf("RootSync %s source error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := k8sobjects.RootSyncObjectV1Beta1(rsName)
			if err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs); err != nil {
				return err
			}
			// Only validate the rendering status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testpredicates.ValidateError(rs.Status.Source.Errors, code, message, nil)
			// syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			// return validateRootSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRootSyncSyncError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSyncError(rsName, code string, message string, resources []v1beta1.ResourceRef, opts ...WaitOption) {
	nt.T.Helper()
	Wait(nt.T, fmt.Sprintf("RootSync %s rendering error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := k8sobjects.RootSyncObjectV1Beta1(rsName)
			err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			// Only validate the sync status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testpredicates.ValidateError(rs.Status.Sync.Errors, code, message, resources)
			// syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			// return validateRootSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
		},
		opts...,
	)
}

// WaitForRepoSyncSyncError waits until the given error (code and message) is present on the RepoSync resource
func (nt *NT) WaitForRepoSyncSyncError(ns, rsName, code string, message string, resources []v1beta1.ResourceRef, opts ...WaitOption) {
	nt.T.Helper()
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s rendering error code %s", ns, rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := k8sobjects.RepoSyncObjectV1Beta1(ns, rsName)
			err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			// Only validate the sync status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testpredicates.ValidateError(rs.Status.Sync.Errors, code, message, resources)
			// syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			// return validateRepoSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
		},
		opts...,
	)
}

// WaitForRepoSyncSourceError waits until the given error (code and message) is present on the RepoSync resource
func (nt *NT) WaitForRepoSyncSourceError(ns, rsName, code, message string, opts ...WaitOption) {
	nt.T.Helper()
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s source error code %s", ns, rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := k8sobjects.RepoSyncObjectV1Beta1(ns, rsName)
			err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			// Only validate the rendering status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testpredicates.ValidateError(rs.Status.Source.Errors, code, message, nil)
			// syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			// return validateRepoSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRootSyncStalledError waits until the given Stalled error is present on the RootSync resource.
func (nt *NT) WaitForRootSyncStalledError(rsName, reason, message string) {
	nt.T.Helper()
	Wait(nt.T, fmt.Sprintf("RootSync %s/%s stalled error", configsync.ControllerNamespace, rsName), nt.DefaultWaitTimeout,
		func() error {
			rs := &v1beta1.RootSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: configsync.ControllerNamespace,
				},
				TypeMeta: k8sobjects.ToTypeMeta(kinds.RootSyncV1Beta1()),
			}
			if err := nt.KubeClient.Get(rsName, configsync.ControllerNamespace, rs); err != nil {
				return err
			}
			stalledCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncStalled)
			if stalledCondition == nil {
				return fmt.Errorf("stalled condition not found")
			}
			if stalledCondition.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected status.conditions['Stalled'].status is True, got %s", stalledCondition.Status)
			}
			if stalledCondition.Reason != reason {
				return fmt.Errorf("expected status.conditions['Stalled'].reason is %s, got %s", reason, stalledCondition.Reason)
			}
			if !strings.Contains(stalledCondition.Message, message) {
				return fmt.Errorf("expected status.conditions['Stalled'].message to contain %s, got %s", message, stalledCondition.Message)
			}
			return nil
		})
}

// WaitForRepoSyncStalledError waits until the given Stalled error is present on the RepoSync resource.
func (nt *NT) WaitForRepoSyncStalledError(rsNamespace, rsName, reason, message string) {
	nt.T.Helper()
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s stalled error", rsNamespace, rsName), nt.DefaultWaitTimeout,
		func() error {
			rs := &v1beta1.RepoSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNamespace,
				},
				TypeMeta: k8sobjects.ToTypeMeta(kinds.RepoSyncV1Beta1()),
			}
			if err := nt.KubeClient.Get(rsName, rsNamespace, rs); err != nil {
				return err
			}
			stalledCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncStalled)
			if stalledCondition == nil {
				return fmt.Errorf("stalled condition not found")
			}
			if stalledCondition.Status != metav1.ConditionTrue {
				return fmt.Errorf("expected status.conditions['Stalled'].status is True, got %s", stalledCondition.Status)
			}
			if stalledCondition.Reason != reason {
				return fmt.Errorf("expected status.conditions['Stalled'].reason is %s, got %s", reason, stalledCondition.Reason)
			}
			if !strings.Contains(stalledCondition.Message, message) {
				return fmt.Errorf("expected status.conditions['Stalled'].message to contain %s, got %s", message, stalledCondition.Message)
			}
			return nil
		})
}

// TODO: Uncomment when Syncing condition consistency is fixed
// func validateRootSyncError(statusErrs []v1beta1.ConfigSyncError, syncingCondition *v1beta1.RootSyncCondition, code string, message string, expectedErrorSourceRefs []v1beta1.ErrorSource) error {
// 	if err := testpredicates.ValidateError(statusErrs, code, message); err != nil {
// 		return err
// 	}
// 	if syncingCondition == nil {
// 		return fmt.Errorf("syncingCondition is nil")
// 	}
// 	if syncingCondition.Status == metav1.ConditionTrue {
// 		return fmt.Errorf("status.conditions['Syncing'].status is True, expected false")
// 	}
// 	if !reflect.DeepEqual(syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs) {
// 		return fmt.Errorf("status.conditions['Syncing'].errorSourceRefs is %v, expected %v", syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs)
// 	}
// 	return nil
// }

// TODO: Uncomment when Syncing condition consistency is fixed
// func validateRepoSyncError(statusErrs []v1beta1.ConfigSyncError, syncingCondition *v1beta1.RepoSyncCondition, code string, message string, expectedErrorSourceRefs []v1beta1.ErrorSource) error {
// 	if err := testpredicates.ValidateError(statusErrs, code, message); err != nil {
// 		return err
// 	}
// 	if syncingCondition == nil {
// 		return fmt.Errorf("syncingCondition is nil")
// 	}
// 	if syncingCondition.Status == metav1.ConditionTrue {
// 		return fmt.Errorf("status.conditions['Syncing'].status is True, expected false")
// 	}
// 	if !reflect.DeepEqual(syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs) {
// 		return fmt.Errorf("status.conditions['Syncing'].errorSourceRefs is %v, expected %v", syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs)
// 	}
// 	return nil
// }
