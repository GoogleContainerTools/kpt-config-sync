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
	"reflect"
	"strings"
	"time"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util/repo"
)

type waitForRepoSyncsOptions struct {
	timeout            time.Duration
	syncNamespaceRepos bool
	rootSha1Fn         Sha1Func
	repoSha1Fn         Sha1Func
	syncDirectoryMap   map[types.NamespacedName]string
}

func newWaitForRepoSyncsOptions(timeout time.Duration, rootFn, repoFn Sha1Func) waitForRepoSyncsOptions {
	return waitForRepoSyncsOptions{
		timeout:            timeout,
		syncNamespaceRepos: true,
		rootSha1Fn:         rootFn,
		repoSha1Fn:         repoFn,
		syncDirectoryMap:   map[types.NamespacedName]string{},
	}
}

// WaitForRepoSyncsOption is an optional parameter for WaitForRepoSyncs.
type WaitForRepoSyncsOption func(*waitForRepoSyncsOptions)

// WithTimeout provides the timeout to WaitForRepoSyncs.
func WithTimeout(timeout time.Duration) WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.timeout = timeout
	}
}

// Sha1Func is the function type that retrieves the commit sha1.
type Sha1Func func(nt *NT, nn types.NamespacedName) (string, error)

// WithRootSha1Func provides the function to get RootSync commit sha1 to WaitForRepoSyncs.
func WithRootSha1Func(fn Sha1Func) WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.rootSha1Fn = fn
	}
}

// WithRepoSha1Func provides the function to get RepoSync commit sha1 to WaitForRepoSyncs.
func WithRepoSha1Func(fn Sha1Func) WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.repoSha1Fn = fn
	}
}

// RootSyncOnly specifies that only the root-sync repo should be synced.
func RootSyncOnly() WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.syncNamespaceRepos = false
	}
}

// SyncDirPredicatePair is a pair of the sync directory and the predicate.
type SyncDirPredicatePair struct {
	Dir       string
	Predicate func(string) Predicate
}

// WithSyncDirectoryMap provides a map of RootSync|RepoSync and the corresponding sync directory.
// The function is used to get the sync directory based on different RootSync|RepoSync name.
func WithSyncDirectoryMap(syncDirectoryMap map[types.NamespacedName]string) WaitForRepoSyncsOption {
	return func(options *waitForRepoSyncsOptions) {
		options.syncDirectoryMap = syncDirectoryMap
	}
}

// syncDirectory returns the sync directory if RootSync|RepoSync is in the map.
// Otherwise, it returns the default sync directory: acme.
func syncDirectory(syncDirectoryMap map[types.NamespacedName]string, nn types.NamespacedName) string {
	if syncDir, found := syncDirectoryMap[nn]; found {
		return syncDir
	}
	return AcmeDir
}

// WaitForRepoSyncs is a convenience method that waits for all repositories
// to sync.
//
// Unless you're testing pre-CSMR functionality related to in-cluster objects,
// you should be using this function to block on ConfigSync to sync everything.
//
// If you want to check the internals of specific objects (e.g. the error field
// of a RepoSync), use nt.Validate() - possibly in a Retry.
func (nt *NT) WaitForRepoSyncs(options ...WaitForRepoSyncsOption) {
	nt.T.Helper()

	waitForRepoSyncsOptions := newWaitForRepoSyncsOptions(nt.DefaultWaitTimeout, DefaultRootSha1Fn, DefaultRepoSha1Fn)
	for _, option := range options {
		option(&waitForRepoSyncsOptions)
	}

	syncTimeout := waitForRepoSyncsOptions.timeout

	if nt.MultiRepo {
		if err := ValidateMultiRepoDeployments(nt); err != nil {
			nt.T.Fatal(err)
		}
		for name := range nt.RootRepos {
			syncDir := syncDirectory(waitForRepoSyncsOptions.syncDirectoryMap, RootSyncNN(name))
			nt.WaitForSync(kinds.RootSyncV1Beta1(), name,
				configmanagement.ControllerNamespace, syncTimeout,
				waitForRepoSyncsOptions.rootSha1Fn, RootSyncHasStatusSyncCommit,
				&SyncDirPredicatePair{syncDir, RootSyncHasStatusSyncDirectory})
		}

		syncNamespaceRepos := waitForRepoSyncsOptions.syncNamespaceRepos
		if syncNamespaceRepos {
			for nn := range nt.NonRootRepos {
				syncDir := syncDirectory(waitForRepoSyncsOptions.syncDirectoryMap, nn)
				nt.WaitForSync(kinds.RepoSyncV1Beta1(), nn.Name, nn.Namespace,
					syncTimeout, waitForRepoSyncsOptions.repoSha1Fn, RepoSyncHasStatusSyncCommit,
					&SyncDirPredicatePair{syncDir, RepoSyncHasStatusSyncDirectory})
			}
		}
	} else {
		nt.WaitForSync(kinds.Repo(), repo.DefaultName, "", syncTimeout,
			waitForRepoSyncsOptions.rootSha1Fn, RepoHasStatusSyncLatestToken, nil)
	}
}

// WaitForSync waits for the specified object to be synced.
//
// o returns a new object of the type to check is synced. It can't just be a
// struct pointer as calling .Get on the same struct pointer multiple times
// has undefined behavior.
//
// name and namespace identify the specific object to check.
//
// timeout specifies the maximum duration allowed for the object to sync.
//
// sha1Func is the function that dynamically computes the expected commit sha1.
//
// syncSha1 is a Predicate to use to tell whether the object is synced as desired.
//
// syncDirPair is a pair of sync dir and the corresponding predicate that tells
// whether it is synced to the expected directory.
// It will skip the validation if it is not provided.
func (nt *NT) WaitForSync(gvk schema.GroupVersionKind, name, namespace string, timeout time.Duration,
	sha1Func Sha1Func, syncSha1 func(string) Predicate, syncDirPair *SyncDirPredicatePair) {
	nt.T.Helper()
	// Wait for the repository to report it is synced.
	took, err := Retry(timeout, func() error {
		var nn types.NamespacedName
		if namespace == "" {
			// If namespace is empty, that is the monorepo mode.
			// So using the sha1 from the default RootSync, not the Repo.
			nn = DefaultRootRepoNamespacedName
		} else {
			nn = types.NamespacedName{
				Name:      name,
				Namespace: namespace,
			}
		}
		sha1, err := sha1Func(nt, nn)
		if err != nil {
			return errors.Wrapf(err, "failed to retrieve sha1")
		}
		isSynced := []Predicate{syncSha1(sha1)}
		if syncDirPair != nil {
			isSynced = append(isSynced, syncDirPair.Predicate(syncDirPair.Dir))
		}
		return nt.ValidateSyncObject(gvk, name, namespace, isSynced...)
	})
	if err != nil {
		nt.T.Logf("failed after %v to wait for %s/%s %v to be synced", took, namespace, name, gvk)

		nt.T.Fatal(err)
	}
	nt.T.Logf("took %v to wait for %s/%s %v to be synced", took, namespace, name, gvk)

	// Automatically renew the Client. We don't have tests that depend on behavior
	// when the test's client is out of date, and if ConfigSync reports that
	// everything has synced properly then (usually) new types should be available.
	if gvk == kinds.Repo() || gvk == kinds.RepoSyncV1Beta1() {
		nt.RenewClient()
	}
}

// WaitForRootSyncSourceError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSourceError(rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s source error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RootSyncObjectV1Beta1(rsName)
			if err := nt.Get(rs.GetName(), rs.GetNamespace(), rs); err != nil {
				return err
			}
			syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			return validateRootSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRootSyncRenderingError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncRenderingError(rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s rendering error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RootSyncObjectV1Beta1(rsName)
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			return validateRootSyncError(rs.Status.Rendering.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.RenderingError})
		},
		opts...,
	)
}

// WaitForRootSyncSyncError waits until the given error (code and message) is present on the RootSync resource
func (nt *NT) WaitForRootSyncSyncError(rsName, code string, message string, ignoreSyncingCondition bool, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RootSync %s rendering error code %s", rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RootSyncObjectV1Beta1(rsName)
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			if ignoreSyncingCondition {
				return validateError(rs.Status.Sync.Errors, code, message)
			}
			syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
			return validateRootSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
		},
		opts...,
	)
}

// WaitForRepoSyncSyncError waits until the given error (code and message) is present on the RepoSync resource
func (nt *NT) WaitForRepoSyncSyncError(ns, rsName, code string, message string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("RepoSync %s/%s rendering error code %s", ns, rsName, code), nt.DefaultWaitTimeout,
		func() error {
			rs := fake.RepoSyncObjectV1Beta1(ns, rsName)
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			return validateRepoSyncError(rs.Status.Sync.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SyncError})
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
			err := nt.Get(rs.GetName(), rs.GetNamespace(), rs)
			if err != nil {
				return err
			}
			syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
			return validateRepoSyncError(rs.Status.Source.Errors, syncingCondition, code, message, []v1beta1.ErrorSource{v1beta1.SourceError})
		},
		opts...,
	)
}

// WaitForRepoSourceError waits until the given error (code and message) is present on the Repo resource
func (nt *NT) WaitForRepoSourceError(code string, opts ...WaitOption) {
	Wait(nt.T, fmt.Sprintf("Repo source error code %s", code), nt.DefaultWaitTimeout,
		func() error {
			obj := &v1.Repo{}
			err := nt.Get(repo.DefaultName, "", obj)
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
			err := nt.Get(repo.DefaultName, "", obj)
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
			err := nt.Get(repo.DefaultName, "", obj)
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
			if err := nt.Get(rsName, rsNamespace, rs); err != nil {
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
			if err := nt.Get(rsName, rsNamespace, rs); err != nil {
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

func validateRootSyncError(statusErrs []v1beta1.ConfigSyncError, syncingCondition *v1beta1.RootSyncCondition, code string, message string, expectedErrorSourceRefs []v1beta1.ErrorSource) error {
	if err := validateError(statusErrs, code, message); err != nil {
		return err
	}
	if syncingCondition == nil {
		return fmt.Errorf("syncingCondition is nil")
	}
	if syncingCondition.Status == metav1.ConditionTrue {
		return fmt.Errorf("status.conditions['Syncing'].status is True, expected false")
	}
	if !reflect.DeepEqual(syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs) {
		return fmt.Errorf("status.conditions['Syncing'].errorSourceRefs is %v, expected %v", syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs)
	}
	return nil
}

func validateRepoSyncError(statusErrs []v1beta1.ConfigSyncError, syncingCondition *v1beta1.RepoSyncCondition, code string, message string, expectedErrorSourceRefs []v1beta1.ErrorSource) error {
	if err := validateError(statusErrs, code, message); err != nil {
		return err
	}
	if syncingCondition == nil {
		return fmt.Errorf("syncingCondition is nil")
	}
	if syncingCondition.Status == metav1.ConditionTrue {
		return fmt.Errorf("status.conditions['Syncing'].status is True, expected false")
	}
	if !reflect.DeepEqual(syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs) {
		return fmt.Errorf("status.conditions['Syncing'].errorSourceRefs is %v, expected %v", syncingCondition.ErrorSourceRefs, expectedErrorSourceRefs)
	}
	return nil
}

func validateError(errs []v1beta1.ConfigSyncError, code string, message string) error {
	if len(errs) == 0 {
		return errors.Errorf("no errors present")
	}
	var codes []string
	for _, e := range errs {
		if e.Code == code {
			if message == "" || strings.Contains(e.ErrorMessage, message) {
				return nil
			}
		}
		codes = append(codes, e.Code)
	}
	return errors.Errorf("error %s not present, got %s", code, strings.Join(codes, ", "))
}
