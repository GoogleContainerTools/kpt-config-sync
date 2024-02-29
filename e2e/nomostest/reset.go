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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// sharedTestNamespaces is a list of namespaces that should not be deleted or
// reset between tests in a shared environment.
//
// Reset will skip deletion of these namespaces, if they exist.
var sharedTestNamespaces = []string{
	configsync.ControllerNamespace,
	configmanagement.RGControllerNamespace,
	configmanagement.MonitoringNamespace,
	testGitNamespace,
	TestRegistryNamespace,
	prometheusNamespace,
}

// protectedNamespaces is a list of namespaces that should never be deleted.
//
// Reset will error if these namespaces exist and have the test label.
//
// Individual test cleanup MUST revert these namespaces, if modified.
// See `checkpointProtectedNamespace` for an example.
var protectedNamespaces = func() []string {
	// Convert official map to list
	list := make([]string, 0, len(differ.SpecialNamespaces))
	for ns := range differ.SpecialNamespaces {
		list = append(list, ns)
	}
	return list
}()

// Reset performs multi-repo test reset:
// - Delete unmanaged RootSyncs & RepoSyncs (with deletion propagation)
// - Validate managed RepoSyncs & RootSyncs are deleted
// - Delete all test namespaces not containing config-sync itself
// - Clear Repository-to-RSync assignments
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
	nsList, err := listNamespaces(nt, withNameNotInListOption(sharedTestNamespaces...))
	if err != nil {
		return err
	}
	// Error if any protected namespace was modified by a test (test label added)
	// and not reverted by the test.
	protectedNamespacesWithTestLabel, nsListItems := filterNamespaces(nsList.Items,
		protectedNamespaces...)
	if len(protectedNamespacesWithTestLabel) > 0 {
		return fmt.Errorf("protected namespace(s) modified by test: %+v",
			protectedNamespacesWithTestLabel)
	}
	if err := ResetNamespaces(nt, nsListItems); err != nil {
		return err
	}
	// Reset any modified system namespaces.
	if err := resetSystemNamespaces(nt); err != nil {
		return err
	}
	// Implicit namespaces may not have the test label. Delete them as well.
	if err := deleteImplicitNamespacesAndWait(nt); err != nil {
		return err
	}

	// NOTE: These git repos are not actually being deleted here, just
	// unassigned to a specific RSync. All remote repos are cached in
	// nt.RemoteRepositories and then reassigned in `resetRepository`.
	// Repos are actually deleted by `Clean` in environment setup and teardown.
	nt.NonRootRepos = make(map[types.NamespacedName]*gitproviders.Repository)
	nt.RootRepos = make(map[string]*gitproviders.Repository)

	// Reset expected objects
	nt.MetricsExpectations.Reset()

	return nil
}

// ResetRootSyncs cleans up one or more RootSyncs and all their managed objects.
// Use this for cleaning up RootSyncs in tests that use delegated control.
func ResetRootSyncs(nt *NT, rsList []v1beta1.RootSync) error {
	nt.T.Logf("[RESET] Deleting RootSyncs (%d)", len(rsList))
	if len(rsList) == 0 {
		return nil
	}

	for _, item := range rsList {
		rs := &item
		rsNN := client.ObjectKeyFromObject(rs)

		if manager, found := rs.GetAnnotations()[string(metadata.ResourceManagerKey)]; found {
			nt.T.Logf("[RESET] RootSync %s managed by %q", rsNN, manager)
			if !IsDeletionPropagationEnabled(rs) {
				// If you go this error, make sure your test cleanup ensures
				// that the managed RootSync has deletion propagation enabled.
				return fmt.Errorf("RootSync %s managed by %q does NOT have deletion propagation enabled: test reset incomplete", rsNN, manager)
			}
			continue
		}

		// Enable deletion propagation, if not enabled
		if EnableDeletionPropagation(rs) {
			nt.T.Logf("[RESET] Enabling deletion propagation on RootSync %s", rsNN)
			if err := nt.KubeClient.Update(rs); err != nil {
				return err
			}
			if err := nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rs.Name, rs.Namespace, []testpredicates.Predicate{
				testpredicates.HasFinalizer(metadata.ReconcilerFinalizer),
			}); err != nil {
				return err
			}
		}

		// Print reconciler logs in case of failure.
		// This ensures the logs are printed, even if the reconciler is deleted.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go TailReconcilerLogs(ctx, nt, RootReconcilerObjectKey(rsNN.Name))

		nt.T.Logf("[RESET] Deleting RootSync %s", rsNN)
		if err := nt.KubeClient.Delete(rs); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	tg := taskgroup.New()
	for _, item := range rsList {
		rs := &item
		rsNN := client.ObjectKeyFromObject(rs)
		nt.T.Logf("[RESET] Waiting for deletion of RootSync %s ...", rsNN)
		tg.Go(func() error {
			return nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), rsNN.Name, rsNN.Namespace)
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
	nt.T.Logf("[RESET] Deleting RepoSyncs (%d)", len(rsList))
	if len(rsList) == 0 {
		// Clean up after `setupDelegatedControl`
		return deleteRepoSyncClusterRole(nt)
	}

	// Apply ClusterRole with the permissions specified by this test.
	rsCR := nt.RepoSyncClusterRole()
	if err := nt.KubeClient.Apply(rsCR); err != nil {
		return err
	}

	for _, item := range rsList {
		rs := &item
		rsNN := client.ObjectKeyFromObject(rs)

		// If managed, skip direct deletion
		if manager, found := rs.GetAnnotations()[string(metadata.ResourceManagerKey)]; found {
			nt.T.Logf("[RESET] RepoSync %s managed by %q", rsNN, manager)
			if !IsDeletionPropagationEnabled(rs) {
				// If you go this error, make sure your test cleanup ensures
				// that the managed RepoSync has deletion propagation enabled.
				return fmt.Errorf("RepoSync %s managed by %q does NOT have deletion propagation enabled: test reset incomplete", rsNN, manager)
			}
			continue
		}

		// Enable deletion propagation, if not enabled
		if EnableDeletionPropagation(rs) {
			nt.T.Logf("[RESET] Enabling deletion propagation on RepoSync %s", rsNN)
			if err := nt.KubeClient.Update(rs); err != nil {
				return err
			}
			if err := nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), rs.Name, rs.Namespace, []testpredicates.Predicate{
				testpredicates.HasFinalizer(metadata.ReconcilerFinalizer),
			}); err != nil {
				return err
			}
		}

		// Grant the reconcile the permissions specified by this test.
		rsCRB := RepoSyncRoleBinding(rsNN)
		if err := nt.KubeClient.Apply(rsCRB); err != nil {
			return err
		}

		// Print reconciler logs in case of failure.
		// This ensures the logs are printed, even if the reconciler is deleted.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go TailReconcilerLogs(ctx, nt, NsReconcilerObjectKey(rsNN.Namespace, rsNN.Name))

		nt.T.Logf("[RESET] Deleting RepoSync %s", rsNN)
		if err := nt.KubeClient.Delete(rs); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
	}
	tg := taskgroup.New()
	for _, item := range rsList {
		obj := &item
		nn := client.ObjectKeyFromObject(obj)
		nt.T.Logf("[RESET] Waiting for deletion of RepoSync %s ...", nn)
		tg.Go(func() error {
			return nt.Watcher.WatchForNotFound(kinds.RepoSyncV1Beta1(), nn.Name, nn.Namespace)
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
	for _, item := range rsList {
		rs := &item
		rsNN := client.ObjectKeyFromObject(rs)
		rbs = append(rbs, RepoSyncRoleBinding(rsNN))
	}
	// Skip deleting managed RoleBindings
	rbs, err := findUnmanaged(nt, rbs...)
	if err != nil {
		return err
	}
	if err := DeleteObjectsAndWait(nt, rbs...); err != nil {
		return err
	}

	return deleteRepoSyncClusterRole(nt)
}

// ResetNamespaces resets one or more Namespaces and all their namespaced objects.
// Use this for resetting Namespaces in tests that use delegated control.
func ResetNamespaces(nt *NT, nsList []corev1.Namespace) error {
	nt.T.Logf("[RESET] Deleting Namespaces (%d)", len(nsList))
	if len(nsList) == 0 {
		return nil
	}
	var objList []client.Object
	for _, item := range nsList {
		obj := &item
		nn := client.ObjectKeyFromObject(obj)

		// If managed, error. The test failed to clean up after itself
		if manager, found := obj.GetAnnotations()[string(metadata.ResourceManagerKey)]; found {
			nt.T.Errorf("[RESET] Failed to reset Namespace %s managed by %q:\n%s", nn, manager, log.AsYAML(obj))
			continue
		}

		objList = append(objList, obj)
	}
	return DeleteObjectsAndWait(nt, objList...)
}

// deleteRepoSyncClusterRole deletes the ClusterRole used by RepoSync
// reconcilers, if it exists.
func deleteRepoSyncClusterRole(nt *NT) error {
	nt.T.Log("[RESET] Deleting RepoSync ClusterRole")
	return DeleteObjectsAndWait(nt, nt.RepoSyncClusterRole())
}

func findUnmanaged(nt *NT, objs ...client.Object) ([]client.Object, error) {
	var unmanaged []client.Object
	for _, obj := range objs {
		if err := nt.KubeClient.Get(obj.GetName(), obj.GetNamespace(), obj); err != nil {
			if !apierrors.IsNotFound(err) {
				return nil, err
			}
		} else if _, found := obj.GetAnnotations()[string(metadata.ResourceManagerKey)]; !found {
			unmanaged = append(unmanaged, obj)
		} // else managed
	}
	return unmanaged, nil
}

func listRootSyncs(nt *NT, opts ...client.ListOption) (*v1beta1.RootSyncList, error) {
	rsList := &v1beta1.RootSyncList{}
	opts = append(opts, withLabelListOption(testkubeclient.TestLabel, testkubeclient.TestLabelValue))
	if err := nt.KubeClient.List(rsList, opts...); err != nil {
		return rsList, err
	}
	return rsList, nil
}

func listRepoSyncs(nt *NT, opts ...client.ListOption) (*v1beta1.RepoSyncList, error) {
	rsList := &v1beta1.RepoSyncList{}
	opts = append(opts, withLabelListOption(testkubeclient.TestLabel, testkubeclient.TestLabelValue))
	if err := nt.KubeClient.List(rsList, opts...); err != nil {
		return rsList, err
	}
	return rsList, nil
}

func listNamespaces(nt *NT, opts ...client.ListOption) (*corev1.NamespaceList, error) {
	nsList := &corev1.NamespaceList{}
	opts = append(opts, withLabelListOption(testkubeclient.TestLabel, testkubeclient.TestLabelValue))
	if err := nt.KubeClient.List(nsList, opts...); err != nil {
		return nsList, err
	}
	return nsList, nil
}

func withLabelListOption(key, value string) client.MatchingLabelsSelector {
	labelSelector := labels.Set{key: value}.AsSelector()
	return client.MatchingLabelsSelector{Selector: labelSelector}
}

func withNameNotInListOption(values ...string) client.MatchingFieldsSelector {
	// The fields package doesn't expose a good way to use the NotIn operator,
	// so we instead AND together a list of != selectors.
	var fieldSelectors []fields.Selector
	for _, ns := range values {
		fieldSelectors = append(fieldSelectors,
			fields.OneTermNotEqualSelector("metadata.name", ns))
	}
	return client.MatchingFieldsSelector{
		Selector: fields.AndSelectors(fieldSelectors...),
	}
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

// ResetRepository creates or re-initializes a remote repository.
func ResetRepository(nt *NT, repoType gitproviders.RepoType, nn types.NamespacedName, sourceFormat filesystem.SourceFormat) *gitproviders.Repository {
	repo := initRepository(nt, repoType, nn, sourceFormat)
	initialCommit(nt, repoType, nn, sourceFormat)
	return repo
}

// initRepository inits a local repository and ensures its remote exists
func initRepository(nt *NT, repoType gitproviders.RepoType, nn types.NamespacedName, sourceFormat filesystem.SourceFormat) *gitproviders.Repository {
	repo, found := nt.RemoteRepositories[nn]
	if !found {
		repo = gitproviders.NewRepository(repoType, nn, sourceFormat, nt.Scheme,
			nt.Logger, nt.GitProvider, nt.TmpDir, nt.gitPrivateKeyPath, nt.DefaultWaitTimeout)
		if err := repo.Create(); err != nil {
			nt.T.Fatal(err)
		}
		nt.RemoteRepositories[nn] = repo
	}
	if err := repo.Init(); err != nil {
		nt.T.Fatal(err)
	}
	return repo
}

// initialCommit creates an initial commit and pushes it to the remote
func initialCommit(nt *NT, repoType gitproviders.RepoType, nn types.NamespacedName, sourceFormat filesystem.SourceFormat) {
	repo, found := nt.RemoteRepositories[nn]
	if !found {
		nt.T.Fatal("repo %s not found", nn.String())
	}
	if err := repo.InitialCommit(sourceFormat); err != nil {
		nt.T.Fatal(err)
	}
	// Reset expected objects.
	// These are used to offset metrics expectations.
	if repoType == gitproviders.RootRepo {
		nt.MetricsExpectations.ResetRootSync(nn.Name)
		nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, nn, repo.MustGet(nt.T, repo.SafetyNSPath))
		nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, nn, repo.MustGet(nt.T, repo.SafetyClusterRolePath))
	} else {
		nt.MetricsExpectations.ResetRepoSync(nn)
	}
}

// TailReconcilerLogs starts tailing a reconciler's logs.
// The logs are stored in memory until either the context is cancelled or the
// kubectl command exits (usually because the container exited).
// This allows capturing logs even if the reconciler is deleted before the
// test ends.
// The logs will only be printed if the test has failed when the command exits.
// Run in an goroutine to capture logs in the background while deleting RSyncs.
func TailReconcilerLogs(ctx context.Context, nt *NT, reconcilerNN types.NamespacedName) {
	out, err := nt.Shell.KubectlContext(ctx, "logs",
		fmt.Sprintf("deployment/%s", reconcilerNN.Name),
		"-n", reconcilerNN.Namespace,
		"-c", reconcilermanager.Reconciler,
		"-f")
	nt.T.Logf("finished tailing logs for %s", reconcilerNN.String())
	// Expect the logs to tail until the context is cancelled, or exit early if
	// the reconciler container exited.
	if err != nil && err.Error() != "signal: killed" {
		// We're only using this for debugging, so don't trigger test failure.
		nt.T.Logf("Failed to tail logs from reconciler %s: %v", reconcilerNN, err)
	}
	// When TailReconcilerLogs is called async, it's possible that the test fails
	// after the kubectl calls returns. For example, when the Pod is evicted the
	// kubectl call will return early. Register a cleanup so that we always log in
	// the case of a failed test.
	nt.T.Cleanup(func() {
		// Only print the logs if the test has failed
		if nt.T.Failed() {
			nt.T.Logf("Reconciler deployment logs (%s):\n%s", reconcilerNN, string(out))
		}
	})
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
