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

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util/repo"
)

type watchForAllSyncsOptions struct {
	timeout            time.Duration
	syncNamespaceRepos bool
	rootSha1Fn         Sha1Func
	repoSha1Fn         Sha1Func
	syncDirectoryMap   map[types.NamespacedName]string
}

// WatchForAllSyncsOptions is an optional parameter for WaitForRepoSyncs.
type WatchForAllSyncsOptions func(*watchForAllSyncsOptions)

// WithTimeout provides the timeout to WaitForRepoSyncs.
func WithTimeout(timeout time.Duration) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.timeout = timeout
	}
}

// Sha1Func is the function type that retrieves the commit sha1.
type Sha1Func func(nt *NT, nn types.NamespacedName) (string, error)

// WithRootSha1Func provides the function to get RootSync commit sha1 to WaitForRepoSyncs.
func WithRootSha1Func(fn Sha1Func) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.rootSha1Fn = fn
	}
}

// WithRepoSha1Func provides the function to get RepoSync commit sha1 to WaitForRepoSyncs.
func WithRepoSha1Func(fn Sha1Func) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.repoSha1Fn = fn
	}
}

// RootSyncOnly specifies that only the root-sync repo should be synced.
func RootSyncOnly() WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.syncNamespaceRepos = false
	}
}

// SyncDirPredicatePair is a pair of the sync directory and the predicate.
type SyncDirPredicatePair struct {
	Dir       string
	Predicate func(string) testpredicates.Predicate
}

// WithSyncDirectoryMap provides a map of RootSync|RepoSync and the corresponding sync directory.
// The function is used to get the sync directory based on different RootSync|RepoSync name.
func WithSyncDirectoryMap(syncDirectoryMap map[types.NamespacedName]string) WatchForAllSyncsOptions {
	return func(options *watchForAllSyncsOptions) {
		options.syncDirectoryMap = syncDirectoryMap
	}
}

// syncDirectory returns the sync directory if RootSync|RepoSync is in the map.
// Otherwise, it returns the default sync directory: acme.
func syncDirectory(syncDirectoryMap map[types.NamespacedName]string, nn types.NamespacedName) string {
	if syncDir, found := syncDirectoryMap[nn]; found {
		return syncDir
	}
	return gitproviders.DefaultSyncDir
}

// WatchForAllSyncs calls WatchForSync on all Syncs in nt.RootRepos & nt.NonRootRepos.
//
// If you want to validate specific fields of a Sync object, use
// nt.Watcher.WatchObject() instead.
func (nt *NT) WatchForAllSyncs(options ...WatchForAllSyncsOptions) error {
	waitForRepoSyncsOptions := watchForAllSyncsOptions{
		timeout:            nt.DefaultWaitTimeout,
		syncNamespaceRepos: true,
		rootSha1Fn:         DefaultRootSha1Fn,
		repoSha1Fn:         DefaultRepoSha1Fn,
		syncDirectoryMap:   map[types.NamespacedName]string{},
	}
	// Override defaults with specified options
	for _, option := range options {
		option(&waitForRepoSyncsOptions)
	}

	if err := ValidateMultiRepoDeployments(nt); err != nil {
		return err
	}

	tg := taskgroup.New()

	for name := range nt.RootRepos {
		nn := RootSyncNN(name)
		syncDir := syncDirectory(waitForRepoSyncsOptions.syncDirectoryMap, nn)
		tg.Go(func() error {
			return nt.WatchForSync(kinds.RootSyncV1Beta1(), nn.Name, nn.Namespace,
				waitForRepoSyncsOptions.rootSha1Fn, RootSyncHasStatusSyncCommit,
				&SyncDirPredicatePair{Dir: syncDir, Predicate: RootSyncHasStatusSyncDirectory},
				testwatcher.WatchTimeout(waitForRepoSyncsOptions.timeout))
		})
	}

	if waitForRepoSyncsOptions.syncNamespaceRepos {
		for nn := range nt.NonRootRepos {
			syncDir := syncDirectory(waitForRepoSyncsOptions.syncDirectoryMap, nn)
			nnPtr := nn
			tg.Go(func() error {
				return nt.WatchForSync(kinds.RepoSyncV1Beta1(), nnPtr.Name, nnPtr.Namespace,
					waitForRepoSyncsOptions.repoSha1Fn, RepoSyncHasStatusSyncCommit,
					&SyncDirPredicatePair{Dir: syncDir, Predicate: RepoSyncHasStatusSyncDirectory},
					testwatcher.WatchTimeout(waitForRepoSyncsOptions.timeout))
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
	syncDirPair *SyncDirPredicatePair,
	opts ...testwatcher.WatchOption,
) error {
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
		return errors.Wrap(err, "failed to retrieve sha1")
	}

	var predicates []testpredicates.Predicate
	predicates = append(predicates, syncSha1(sha1))
	if syncDirPair != nil {
		predicates = append(predicates, syncDirPair.Predicate(syncDirPair.Dir))
	}

	err = nt.Watcher.WatchObject(gvk, name, namespace, predicates, opts...)
	if err != nil {
		return errors.Wrap(err, "waiting for sync")
	}
	nt.T.Logf("%s %s/%s is synced", gvk.Kind, namespace, name)
	return nil
}

// WaitForRootSyncSourceError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSourceError(rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s source error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := fake.RootSyncObjectV1Beta1(rsName)
			if err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs); err != nil {
				return err
			}
			// Only validate the rendering status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testutils.ValidateError(rs.Status.Source.Errors, code, message)
			// syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			// return validateRootSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRootSyncRenderingError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncRenderingError(rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s rendering error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := fake.RootSyncObjectV1Beta1(rsName)
			err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			// Only validate the rendering status, not the Syncing condition
			// TODO: Revert this hack once async sync status updates are fixed to include rendering errors
			return testutils.ValidateError(rs.Status.Rendering.Errors, code, message)
			// syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			// return validateRootSyncError(rs.Status.Rendering.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.RenderingError})
		},
		opts...,
	)
}

// WaitForRootSyncSyncError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSyncError(rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s rendering error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := fake.RootSyncObjectV1Beta1(rsName)
			err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			// Only validate the sync status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testutils.ValidateError(rs.Status.Sync.Errors, code, message)
			// syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			// return validateRootSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
		},
		opts...,
	)
}

// WaitForRepoSyncSyncError waits until the given error (code and message) is present on the RepoSync resource
func (nt *NT) WaitForRepoSyncSyncError(ns, rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s rendering error code %s", ns, rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := fake.RepoSyncObjectV1Beta1(ns, rsName)
			err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			// Only validate the sync status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testutils.ValidateError(rs.Status.Sync.Errors, code, message)
			// syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			// return validateRepoSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
		},
		opts...,
	)
}

// WaitForRepoSyncSourceError waits until the given error (code and message) is present on the RepoSync resource
func (nt *NT) WaitForRepoSyncSourceError(ns, rsName, code, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s source error code %s", ns, rsName, code), nt.DefaultWaitTimeout,
		func() error {
			nt.T.Helper()
			rs := fake.RepoSyncObjectV1Beta1(ns, rsName)
			err := nt.KubeClient.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			// Only validate the rendering status, not the Syncing condition
			// TODO: Remove this hack once async sync status updates are fixed to reflect only the latest commit.
			return testutils.ValidateError(rs.Status.Source.Errors, code, message)
			// syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			// return validateRepoSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRepoSourceError waits until the given error (code and message) is present on the Repo resource
func (nt *NT) WaitForRepoSourceError(code string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("Repo source error code %s", code), nt.DefaultWaitTimeout,
		func() error {
			obj := &v1.Repo{}
			err := nt.KubeClient.Get(repo.DefaultName, "", obj)
			if err != nil {
				return err
			}
			errs := obj.Status.Source.Errors
			if len(errs) == 0 {
				return errors.Errorf("no errors present")
			}
			var codes []string
			for _, e := range errs {
				if e.Code == fmt.Sprintf("KNV%s", code) {
					return nil
				}
				codes = append(codes, e.Code)
			}
			return errors.Errorf("error %s not present, got %s", code, strings.Join(codes, ", "))
		},
		opts...,
	)
}

// WaitForRepoSourceErrorClear waits until the given error code disappears from the Repo resource
func (nt *NT) WaitForRepoSourceErrorClear(opts ...WaitOption) {
	Wait(nt.T, "Repo source errors cleared", nt.DefaultWaitTimeout,
		func() error {
			obj := &v1.Repo{}
			err := nt.KubeClient.Get(repo.DefaultName, "", obj)
			if err != nil {
				return err
			}
			errs := obj.Status.Source.Errors
			if len(errs) == 0 {
				return nil
			}
			var messages []string
			for _, e := range errs {
				messages = append(messages, e.ErrorMessage)
			}
			return errors.Errorf("got errors %s", strings.Join(messages, ", "))
		},
		opts...,
	)
}

// WaitForRepoImportErrorCode waits until the given error code is present on the Repo resource.
func (nt *NT) WaitForRepoImportErrorCode(code string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("Repo import error code %s", code), nt.DefaultWaitTimeout,
		func() error {
			obj := &v1.Repo{}
			err := nt.KubeClient.Get(repo.DefaultName, "", obj)
			if err != nil {
				return err
			}
			errs := obj.Status.Import.Errors
			if len(errs) == 0 {
				return errors.Errorf("no errors present")
			}
			var codes []string
			for _, e := range errs {
				if e.Code == fmt.Sprintf("KNV%s", code) {
					return nil
				}
				codes = append(codes, e.Code)
			}
			return errors.Errorf("error %s not present, got %s", code, strings.Join(codes, ", "))
		},
		opts...,
	)
}

// WaitForRootSyncStalledError waits until the given Stalled error is present on the RootSync resource.
func (nt *NT) WaitForRootSyncStalledError(rsNamespace, rsName, reason, message string) {
	Wait(nt.T, fmt.Sprintf("RootSync %s/%s stalled error", rsNamespace, rsName), nt.DefaultWaitTimeout,
		func() error {
			rs := &v1beta1.RootSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNamespace,
				},
				TypeMeta: fake.ToTypeMeta(kinds.RootSyncV1Beta1()),
			}
			if err := nt.KubeClient.Get(rsName, rsNamespace, rs); err != nil {
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
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s stalled error", rsNamespace, rsName), nt.DefaultWaitTimeout,
		func() error {
			rs := &v1beta1.RepoSync{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rsName,
					Namespace: rsNamespace,
				},
				TypeMeta: fake.ToTypeMeta(kinds.RepoSyncV1Beta1()),
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
// 	if err := testutils.ValidateError(statusErrs, code, message); err != nil {
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
// 	if err := testutils.ValidateError(statusErrs, code, message); err != nil {
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
