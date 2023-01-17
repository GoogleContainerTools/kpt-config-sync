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
	"context"
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Reset performs multi-repo test reset:
// - Delete unmanaged RootSyncs & RepoSyncs (with deletion propagation)
// - Validate managed RepoSyncs & RootSyncs are deleted
// - Delete all test namespaces not containing config-sync itself
// - Delete NonRootRepos & RootRepos (except the default RootRepo)
//
// This should cleanly delete or reset all registered RSyncs.
// Any managed RSyncs must have deletion propagation enabled by the test that
// created them, otherwise their managed resources will not be deleted when the
// RSync is deleted.
// Any unregistered Repos must be reset by individual test Cleanup.
func Reset(nt *NT) error {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		nt.T.Logf("[RESET] Test environment reset took %v", elapsed)
	}()

	// Delete all existing RootSyncs with the test label.
	// Enable deletion propagation first, to clean up managed resources.
	rootSyncList, err := listRootSyncs(nt)
	if err != nil {
		return err
	}
	if err := ResetRootSyncs(nt, rootSyncList.Items); err != nil {
		return err
	}

	// Delete all existing RepoSyncs with the test label.
	// Enable deletion propagation first, to clean up managed resources.
	repoSyncList, err := listRepoSyncs(nt)
	if err != nil {
		return err
	}
	if err := ResetRepoSyncs(nt, repoSyncList.Items); err != nil {
		return err
	}

	// Delete all Namespaces with the test label (except shared).
	nsList, err := listNamespaces(nt)
	if err != nil {
		return err
	}
	// Error if any protected namespace was modified by a test (test label added)
	// and not reverted by the test.
	protectedNamespaces, nsListItems := filterNamespaces(nsList.Items,
		protectedNamespaceList()...)
	if len(protectedNamespaces) > 0 {
		return errors.Errorf("protected namespace(s) modified by test: %+v",
			protectedNamespaces)
	}
	// Skip deleting namespaces that contain test infra or Config Sync itself.
	_, nsListItems = filterNamespaces(nsListItems,
		configsync.ControllerNamespace,
		configmanagement.RGControllerNamespace,
		metrics.MonitoringNamespace,
		testGitNamespace)
	if err := ResetNamespaces(nt, nsListItems); err != nil {
		return err
	}

	// NOTE: These git repos are not actually being deleted here, just forgotten.
	// Repos are deleted by `Clean` for environment setup and teardown.

	nt.T.Log("[RESET] Resetting NonRootRepos")
	nt.NonRootRepos = make(map[types.NamespacedName]*Repository)

	nt.T.Log("[RESET] Resetting RootRepos")
	// Retain the root-sync repo (speeds up reset a bit when there are no changes)
	rootRepo := nt.RootRepos[configsync.RootSyncName]
	nt.RootRepos = make(map[string]*Repository)
	nt.RootRepos[configsync.RootSyncName] = rootRepo

	return nil
}

func protectedNamespaceList() []string {
	list := make([]string, 0, len(differ.SpecialNamespaces))
	for ns := range differ.SpecialNamespaces {
		list = append(list, ns)
	}
	return list
}

type resetRecord struct {
	Objects          map[types.NamespacedName]struct{}
	ManagedObjects   map[types.NamespacedName]struct{}
	ObjectNamespaces map[string]struct{}
}

func newResetRecord() *resetRecord {
	return &resetRecord{
		Objects:          make(map[types.NamespacedName]struct{}),
		ManagedObjects:   make(map[types.NamespacedName]struct{}),
		ObjectNamespaces: make(map[string]struct{}),
	}
}

// ResetRootSyncs cleans up one or more RootSyncs and all their managed objects.
// Use this for cleaning up RootSyncs in tests that use delegated control.
func ResetRootSyncs(nt *NT, rsList []v1beta1.RootSync) error {
	if len(rsList) == 0 {
		return nil
	}

	nt.T.Logf("[RESET] Deleting RootSyncs (%d)", len(rsList))

	record := newResetRecord()

	for _, item := range rsList {
		rs := &item
		rsNN := client.ObjectKeyFromObject(rs)
		record.Objects[rsNN] = struct{}{}
		record.ObjectNamespaces[rsNN.Namespace] = struct{}{}

		if manager, found := rs.GetAnnotations()[string(metadata.ResourceManagerKey)]; found {
			record.ManagedObjects[rsNN] = struct{}{}
			nt.T.Logf("[RESET] RootSync %s managed by %q", rsNN, manager)
			if !IsDeletionPropagationEnabled(rs) {
				// If you go this error, make sure your test cleanup ensures
				// that the managed RootSync has deletion propagation enabled.
				return errors.Errorf("RootSync %s managed by %q does NOT have deletion propagation enabled: test reset incomplete", rsNN, manager)
			}
			continue
		}

		// Enable deletion propagation, if not enabled
		if EnableDeletionPropagation(rs) {
			nt.T.Logf("[RESET] Enabling deletion propagation on RootSync %s", rsNN)
			if err := nt.Update(rs); err != nil {
				return err
			}
			if err := WatchObject(nt, kinds.RootSyncV1Beta1(), rs.Name, rs.Namespace, []Predicate{
				HasFinalizer(metadata.ReconcilerFinalizer),
			}); err != nil {
				return err
			}
		}

		// Print reconciler logs in case of failure.
		// This ensures the logs are printed, even if the reconciler is deleted.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go TailReconcilerLogs(ctx, nt, RootReconcilerObjectKey(rsNN.Name))

		// DeletePropagationBackground is required when deleting RSyncs with
		// dependencies that have owners references. Otherwise the reconciler
		// and dependenencies will be garbage collected before the finalizer
		// can delete the managed resources.
		// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
		nt.T.Logf("[RESET] Deleting RootSync %s", rsNN)
		if err := nt.Delete(rs, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			return err
		}
	}
	tg := taskgroup.New()
	for _, item := range rsList {
		rs := &item
		rsNN := client.ObjectKeyFromObject(rs)
		nt.T.Logf("[RESET] Waiting for deletion of RootSync %s ...", rsNN)
		tg.Go(func() error {
			return WatchForNotFound(nt, kinds.RootSyncV1Beta1(), rs.Name, rs.Namespace)
		})
	}
	return tg.Wait()
}

// ResetRepoSyncs cleans up one or more RepoSyncs and all their managed objects.
// Use this for cleaning up RepoSyncs in tests that use delegated control.
//
// To ensure the reconcile finalizer has permission to delete managed resources,
// ClusterRole and RoleBindings will be created and then later deleted.
// This also cleans up any CRs, RBs, and CRBs left behind by delegated control.
func ResetRepoSyncs(nt *NT, rsList []v1beta1.RepoSync) error {
	if len(rsList) == 0 {
		// Clean up after `setupDelegatedControl`
		return deleteRepoSyncClusterRole(nt)
	}

	nt.T.Logf("[RESET] Deleting RepoSyncs (%d)", len(rsList))

	record := newResetRecord()

	// Create ClusterRole, if not found.
	rsCR := nt.RepoSyncClusterRole()
	if err := nt.Create(rsCR); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
	}

	for _, item := range rsList {
		rs := &item
		rsNN := client.ObjectKeyFromObject(rs)
		record.Objects[rsNN] = struct{}{}
		record.ObjectNamespaces[rsNN.Namespace] = struct{}{}

		// If managed, skip direct deletion
		if manager, found := rs.GetAnnotations()[string(metadata.ResourceManagerKey)]; found {
			record.ManagedObjects[rsNN] = struct{}{}
			nt.T.Logf("[RESET] RepoSync %s managed by %q", rsNN, manager)
			if !IsDeletionPropagationEnabled(rs) {
				// If you go this error, make sure your test cleanup ensures
				// that the managed RepoSync has deletion propagation enabled.
				return errors.Errorf("RepoSync %s managed by %q does NOT have deletion propagation enabled: test reset incomplete", rsNN, manager)
			}
			continue
		}

		// Enable deletion propagation, if not enabled
		if EnableDeletionPropagation(rs) {
			nt.T.Logf("[RESET] Enabling deletion propagation on RepoSync %s", rsNN)
			if err := nt.Update(rs); err != nil {
				return err
			}
			if err := WatchObject(nt, kinds.RepoSyncV1Beta1(), rs.Name, rs.Namespace, []Predicate{
				HasFinalizer(metadata.ReconcilerFinalizer),
			}); err != nil {
				return err
			}
		}

		// Create RoleBinding, if not found (ensure finalizer has permission)
		rsCRB := RepoSyncRoleBinding(rsNN)
		if err := nt.Create(rsCRB); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return err
			}
		}

		// Print reconciler logs in case of failure.
		// This ensures the logs are printed, even if the reconciler is deleted.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go TailReconcilerLogs(ctx, nt, NsReconcilerObjectKey(rsNN.Namespace, rsNN.Name))

		// DeletePropagationBackground is required when deleting RSyncs with
		// dependencies that have owners references. Otherwise the reconciler
		// and dependenencies will be garbage collected before the finalizer
		// can delete the managed resources.
		// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
		nt.T.Logf("[RESET] Deleting RepoSync %s", rsNN)
		if err := nt.Delete(rs, client.PropagationPolicy(metav1.DeletePropagationBackground)); err != nil {
			return err
		}
	}
	tg := taskgroup.New()
	for _, item := range rsList {
		obj := &item
		nn := client.ObjectKeyFromObject(obj)
		nt.T.Logf("[RESET] Waiting for deletion of RepoSync %s ...", nn)
		tg.Go(func() error {
			return WatchForNotFound(nt, kinds.RepoSyncV1Beta1(), obj.Name, obj.Namespace)
		})
	}
	if err := tg.Wait(); err != nil {
		return err
	}

	// Delete any RoleBindings left behind.
	// For central control, the parent RSync _should_ handle deleting the RB,
	// but for delegated control and other edge cases clean them up regardless.
	nt.T.Log("[RESET] Deleting test RoleBindings")
	var rbs []client.Object
	for rsNN := range record.Objects {
		rbs = append(rbs, RepoSyncRoleBinding(rsNN))
	}
	// Skip deleting managed RoleBindings
	rbs, err := findUnmanaged(nt, rbs...)
	if err != nil {
		return err
	}
	if err := batchDeleteAndWait(nt, rbs...); err != nil {
		return err
	}

	// Delete any ClusterRoleBindings left behind.
	// CRBs are usually only applied if PSP was enabled, but clean them up regardless.
	nt.T.Log("[RESET] Deleting test ClusterRoleBindings")
	var crbs []client.Object
	for rsNN := range record.Objects {
		crbs = append(crbs, repoSyncClusterRoleBinding(rsNN))
	}
	// Skip deleting managed ClusterRoleBindings
	crbs, err = findUnmanaged(nt, crbs...)
	if err != nil {
		return err
	}
	if err := batchDeleteAndWait(nt, crbs...); err != nil {
		return err
	}

	return deleteRepoSyncClusterRole(nt)
}

// ResetNamespaces cleans up one or more Namespaces and all their namespaced objects.
// Use this for cleaning up Namespaces in tests that use delegated control.
func ResetNamespaces(nt *NT, nsList []corev1.Namespace) error {
	if len(nsList) == 0 {
		return nil
	}

	nt.T.Logf("[RESET] Deleting Namespaces (%d)", len(nsList))

	record := newResetRecord()

	for _, item := range nsList {
		obj := &item
		nn := client.ObjectKeyFromObject(obj)
		record.Objects[nn] = struct{}{}
		record.ObjectNamespaces[nn.Namespace] = struct{}{}

		// If managed, skip direct deletion
		if manager, found := obj.GetAnnotations()[string(metadata.ResourceManagerKey)]; found {
			record.ManagedObjects[nn] = struct{}{}
			nt.T.Logf("[RESET] Namespace %s managed by %q", nn, manager)
			continue
		}

		nt.T.Logf("[RESET] Deleting Namespace %s", nn)
		if err := nt.Delete(obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
			return err
		}
	}
	for _, item := range nsList {
		obj := &item
		nn := client.ObjectKeyFromObject(obj)
		nt.T.Logf("[RESET] Waiting for deletion of Namespace %s ...", nn)
		if err := WatchForNotFound(nt, kinds.Namespace(), obj.Name, obj.Namespace); err != nil {
			return err
		}
	}

	return nil
}

// deleteRepoSyncClusterRole deletes the ClusterRole used by RepoSync
// reconcilers, if it exists.
func deleteRepoSyncClusterRole(nt *NT) error {
	nt.T.Log("[RESET] Deleting RepoSync ClusterRole")
	return batchDeleteAndWait(nt, nt.RepoSyncClusterRole())
}

func findUnmanaged(nt *NT, objs ...client.Object) ([]client.Object, error) {
	var unmanaged []client.Object
	for _, obj := range objs {
		if err := nt.Get(obj.GetName(), obj.GetNamespace(), obj); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, err
			}
		} else if _, found := obj.GetAnnotations()[string(metadata.ResourceManagerKey)]; !found {
			unmanaged = append(unmanaged, obj)
		} // else managed
	}
	return unmanaged, nil
}

func batchDeleteAndWait(nt *NT, objs ...client.Object) error {
	for _, obj := range objs {
		if err := nt.Delete(obj); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	tg := taskgroup.New()
	for _, obj := range objs {
		gvk, err := kinds.Lookup(obj, nt.scheme)
		if err != nil {
			return err
		}
		tg.Go(func() error {
			return WatchForNotFound(nt, gvk, obj.GetName(), obj.GetNamespace())
		})
	}
	return tg.Wait()
}

func listRootSyncs(nt *NT, opts ...client.ListOption) (*v1beta1.RootSyncList, error) {
	rsList := &v1beta1.RootSyncList{}
	opts = append(opts, withLabelListOption(TestLabel, TestLabelValue))
	if err := nt.List(rsList, opts...); err != nil {
		return rsList, err
	}
	return rsList, nil
}

func listRepoSyncs(nt *NT, opts ...client.ListOption) (*v1beta1.RepoSyncList, error) {
	rsList := &v1beta1.RepoSyncList{}
	opts = append(opts, withLabelListOption(TestLabel, TestLabelValue))
	if err := nt.List(rsList, opts...); err != nil {
		return rsList, err
	}
	return rsList, nil
}

func listNamespaces(nt *NT, opts ...client.ListOption) (*corev1.NamespaceList, error) {
	nsList := &corev1.NamespaceList{}
	opts = append(opts, withLabelListOption(TestLabel, TestLabelValue))
	if err := nt.List(nsList, opts...); err != nil {
		return nsList, err
	}
	return nsList, nil
}

func withLabelListOption(key, value string) client.MatchingLabelsSelector {
	labelSelector := labels.Set{key: value}.AsSelector()
	return client.MatchingLabelsSelector{Selector: labelSelector}
}

func filterNamespaces(nsList []corev1.Namespace, excludes ...string) (found []corev1.Namespace, remaining []corev1.Namespace) {
	for _, ns := range nsList {
		if stringSliceContains(excludes, ns.Name) {
			found = append(found, ns)
		} else {
			remaining = append(remaining, ns)
		}
	}
	return found, remaining
}

func stringSliceContains(list []string, value string) bool {
	for _, elem := range list {
		if elem == value {
			return true
		}
	}
	return false
}

// resetRepository re-initializes an existing remote repository or creates a new remote repository.
func resetRepository(nt *NT, repoType RepoType, nn types.NamespacedName, sourceFormat filesystem.SourceFormat) *Repository {
	if repo, found := nt.RemoteRepositories[nn]; found {
		repo.ReInit(nt, sourceFormat)
		return repo
	}
	return NewRepository(nt, repoType, nn, sourceFormat)
}

// resetRootRepo resets the RootSync and its Repository, but only if they need
// to change.
func resetRootRepo(nt *NT, rsName string, sourceFormat filesystem.SourceFormat) error {
	// Reset the Git repo.
	// This may cause temporary errors in the RootSync until it's reconfigured too.
	nt.RootRepos[rsName] = resetRepository(nt, RootRepo, RootSyncNN(rsName), sourceFormat)
	defaultRS := RootSyncObjectV1Beta1FromRootRepo(nt, rsName)

	rs := &v1beta1.RootSync{}
	err := nt.Get(defaultRS.Name, defaultRS.Namespace, rs)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		// RootSync NotFound
		// This can happen if it was managed by another RootSync which has
		// already been reset.
		nt.T.Logf("Creating new RootSync (%s): %s", defaultRS.Name, log.AsJSON(defaultRS.Spec))
		err = nt.Create(defaultRS)
		if err != nil {
			return err
		}
		return WatchForCurrentStatus(nt, kinds.RootSyncV1Beta1(), defaultRS.Name, defaultRS.Namespace)
	}

	// Avoid unnecessary updates
	if cmp.Equal(rs.Spec, defaultRS.Spec) {
		nt.T.Logf("Skipping reset for RootSync (%s): no change required", defaultRS.Name)
		return nil
	}

	// Reset the RootSync spec to match the config from nt.RootRepos
	defaultRS.Spec.DeepCopyInto(&rs.Spec)
	nt.T.Logf("Resetting RootSync (%s): %s", rs.Name, log.AsJSON(rs.Spec))
	if err = nt.Update(rs); err != nil {
		return err
	}
	return WatchForCurrentStatus(nt, kinds.RootSyncV1Beta1(), rs.Name, rs.Namespace)
}

// TailReconcilerLogs starts tailing a reconciler's logs.
// The logs are stored in memory until either the context is cancelled or the
// kubectl command exits (usually because the container exited).
// This allows capturing logs even if the reconciler is deleted before the
// test ends.
// The logs will only be printed if the test has failed when the command exits.
// Run in an goroutine to capture logs in the background while deleting RSyncs.
func TailReconcilerLogs(ctx context.Context, nt *NT, reconcilerNN types.NamespacedName) {
	out, err := nt.KubectlContext(ctx, "logs",
		fmt.Sprintf("deployment/%s", reconcilerNN.Name),
		"-n", reconcilerNN.Namespace,
		"-c", reconcilermanager.Reconciler,
		"-f")
	// Expect the logs to tail until the context is cancelled, or exit early if
	// the reconciler container exited.
	if err != nil && err.Error() != "signal: killed" {
		// We're only using this for debugging, so don't trigger test failure.
		nt.T.Logf("Failed to tail logs from reconciler %s: %v", reconcilerNN, err)
	}
	// Only print the logs if the test has failed
	if nt.T.Failed() {
		nt.T.Logf("Reconciler deployment logs (%s):\n%s", reconcilerNN, string(out))
	}
}

// RootReconcilerObjectKey returns an ObjectKey for interracting with the
// RootReconciler for the specified RootSync.
func RootReconcilerObjectKey(syncName string) client.ObjectKey {
	return client.ObjectKey{
		Name:      core.RootReconcilerName(syncName),
		Namespace: configsync.ControllerNamespace,
	}
}

// NsReconcilerObjectKey returns an ObjectKey for interracting with the
// NsReconciler for the specified RepoSync.
func NsReconcilerObjectKey(namespace, syncName string) client.ObjectKey {
	return client.ObjectKey{
		Name:      core.NsReconcilerName(namespace, syncName),
		Namespace: configsync.ControllerNamespace,
	}
}
