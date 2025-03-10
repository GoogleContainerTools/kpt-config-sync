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

package e2e

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// testNs is the namespace of all RepoSync objects.
const testNs = "test-ns"

// TestMultiSyncs_Unstructured_MixedControl tests multiple syncs created in the mixed control mode.
// - rootSync0 is created using k8s api.
// - rootSync1 is created using k8s api. This is to validate multiple RootSyncs can be created in the delegated mode.
// - rootSync2 is a v1alpha1 version of RootSync declared in the root repo of root-sync. This is to validate RootSync can be managed in a root repo and validate the v1alpha1 version API.
// - rootSync3 is a v1alpha1 version of RootSync declared in rs-2. This is to validate RootSync can be managed in a different root repo and validate the v1alpha1 version API.
// - repoSync1 is created using k8s api. This is to validate RepoSyncs can be created in the delegated mode.
// - repoSync2 is a v1alpha1 version of RepoSync created using k8s api. This is to validate v1alpha1 version of RepoSync can be created in the delegated mode.
// - repoSync3 is declared in the root repo of root-sync. This is to validate RepoSync can be managed in a root repo.
// - repoSync4 is a v1alpha1 version of RepoSync declared in the namespace repo of nn2. This is to validate RepoSync can be managed in a namespace repo in the same namespace.
// - repoSync5 is declared in the root repo of rr1. This is to validate implicit namespace won't cause conflict between two root reconcilers (rr1 and root-sync).
// - repoSync6 is created using k8s api in a different namespace but with the same name "nr1".
func TestMultiSyncs_Unstructured_MixedControl(t *testing.T) {
	rootSync0ID := nomostest.DefaultRootSyncID
	rootSync1ID := core.RootSyncID("rr1")
	rootSync2ID := core.RootSyncID("rr2")
	rootSync3ID := core.RootSyncID("rr3")
	repoSync1ID := core.RepoSyncID("nr1", testNs)
	repoSync2ID := core.RepoSyncID("nr2", testNs)
	repoSync3ID := core.RepoSyncID("nr3", testNs)
	repoSync4ID := core.RepoSyncID("nr4", testNs)
	repoSync5ID := core.RepoSyncID("nr5", testNs)
	repoSync1Key := repoSync1ID.ObjectKey
	repoSync2Key := repoSync2ID.ObjectKey
	repoSync3Key := repoSync3ID.ObjectKey
	repoSync4Key := repoSync4ID.ObjectKey
	repoSync5Key := repoSync5ID.ObjectKey
	testNs2 := "ns-2"
	repoSync6ID := core.RepoSyncID(repoSync1ID.Name, testNs2)

	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.WithDelegatedControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.SyncWithGitSource(rootSync1ID, ntopts.Unstructured),
		// NS reconciler allowed to manage RepoSyncs but not RoleBindings
		ntopts.RepoSyncPermissions(policy.RepoSyncAdmin()),
		ntopts.SyncWithGitSource(repoSync1ID),
		ntopts.SyncWithGitSource(repoSync6ID))
	rootSync0GitRepo := nt.SyncSourceGitReadWriteRepository(rootSync0ID)
	rootSync1GitRepo := nt.SyncSourceGitReadWriteRepository(rootSync1ID)

	// Cleanup all unmanaged RepoSyncs BEFORE the root-sync is deleted!
	// Otherwise, the test Namespace will be deleted while still containing
	// RepoSyncs, which could block deletion if their finalizer hangs.
	// This also replaces depends-on deletion ordering (RoleBinding -> RepoSync),
	// which can't be used by unmanaged syncs or objects in different repos.
	nt.T.Cleanup(func() {
		nt.T.Log("[CLEANUP] Deleting test RepoSyncs")
		var rsList []v1beta1.RepoSync
		rsNNs := []types.NamespacedName{repoSync1Key, repoSync2Key, repoSync4Key}
		for _, rsNN := range rsNNs {
			rs := &v1beta1.RepoSync{}
			err := nt.KubeClient.Get(rsNN.Name, rsNN.Namespace, rs)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					nt.T.Error(err)
				}
			} else {
				rsList = append(rsList, *rs)
			}
		}
		if err := nomostest.ResetRepoSyncs(nt, rsList); err != nil {
			nt.T.Error(err)
		}
	})

	resetExpectedGitSync(nt, rootSync2ID)
	resetExpectedGitSync(nt, rootSync3ID)
	resetExpectedGitSync(nt, repoSync2ID)
	resetExpectedGitSync(nt, repoSync3ID)
	resetExpectedGitSync(nt, repoSync4ID)
	resetExpectedGitSync(nt, repoSync5ID)

	rootSync2GitRepo := nt.SyncSourceGitReadWriteRepository(rootSync2ID)
	repoSync2GitRepo := nt.SyncSourceGitReadWriteRepository(repoSync2ID)

	nrb2 := nomostest.RepoSyncRoleBinding(repoSync2Key)
	nrb3 := nomostest.RepoSyncRoleBinding(repoSync3Key)
	nrb4 := nomostest.RepoSyncRoleBinding(repoSync4Key)
	nrb5 := nomostest.RepoSyncRoleBinding(repoSync5Key)

	nt.T.Logf("Adding Namespace & RoleBindings for RepoSyncs")
	nt.Must(rootSync0GitRepo.Add(fmt.Sprintf("acme/cluster/ns-%s.yaml", testNs), k8sobjects.NamespaceObject(testNs)))
	nt.Must(rootSync0GitRepo.Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, repoSync2Key.Name), nrb2))
	nt.Must(rootSync0GitRepo.Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, repoSync4Key.Name), nrb4))
	nt.Must(rootSync0GitRepo.CommitAndPush("Adding Namespace & RoleBindings for RepoSyncs"))

	nt.T.Logf("Add RootSync %s to the repository of RootSync %s", rootSync2ID.Name, configsync.RootSyncName)

	rs2 := nomostest.RootSyncObjectV1Alpha1FromRootRepo(nt, rootSync2ID.Name)
	nt.Must(rootSync0GitRepo.Add(fmt.Sprintf("acme/rootsyncs/%s.yaml", rootSync2ID.Name), rs2))
	nt.Must(rootSync0GitRepo.CommitAndPush("Adding RootSync: " + rootSync2ID.Name))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new rootSync2.
	nt.Must(nt.WatchForAllSyncs(
		nomostest.SkipRootRepos(rootSync3ID.Name),
		nomostest.SkipNonRootRepos(repoSync2Key, repoSync3Key, repoSync4Key, repoSync5Key)))

	nt.T.Logf("Add RootSync %s to the repository of RootSync %s", rootSync3ID.Name, rootSync2ID.Name)
	rs3 := nomostest.RootSyncObjectV1Alpha1FromRootRepo(nt, rootSync3ID.Name)
	nt.Must(rootSync2GitRepo.Add(fmt.Sprintf("acme/rootsyncs/%s.yaml", rootSync3ID.Name), rs3))
	nt.Must(rootSync2GitRepo.CommitAndPush("Adding RootSync: " + rootSync3ID.Name))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new rootSync3.
	nt.Must(nt.WatchForAllSyncs(
		nomostest.SkipNonRootRepos(repoSync2Key, repoSync3Key, repoSync4Key, repoSync5Key)))

	nt.T.Logf("Create RepoSync %s", repoSync2Key)
	nrs2 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, repoSync2Key)
	nt.Must(nt.KubeClient.Create(nrs2))
	// RoleBinding (nrb2) managed by RootSync root-sync, because the namespace
	// tenant does not have permission to manage RBAC.
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr2.
	nt.Must(nt.WatchForAllSyncs(
		nomostest.SkipNonRootRepos(repoSync3Key, repoSync4Key, repoSync5Key)))

	nt.T.Logf("Add RepoSync %s to RootSync %s", repoSync3Key, configsync.RootSyncName)
	nrs3 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, repoSync3Key)
	// Ensure the RoleBinding is deleted after the RepoSync
	nt.Must(nomostest.SetDependencies(nrs3, nt.Scheme, nrb3))
	nt.Must(rootSync0GitRepo.Add(fmt.Sprintf("acme/reposyncs/%s.yaml", repoSync3Key.Name), nrs3))
	nt.Must(rootSync0GitRepo.Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, repoSync3Key.Name), nrb3))
	nt.Must(rootSync0GitRepo.CommitAndPush("Adding RepoSync: " + repoSync3Key.String()))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr3.
	nt.Must(nt.WatchForAllSyncs(
		nomostest.SkipNonRootRepos(repoSync4Key, repoSync5Key)))

	nt.T.Logf("Add RepoSync %s to RepoSync %s", repoSync4Key, repoSync2Key)
	nrs4 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, repoSync4Key)
	nt.Must(repoSync2GitRepo.Add(fmt.Sprintf("acme/reposyncs/%s.yaml", repoSync4Key.Name), nrs4))
	// RoleBinding (nrb4) managed by RootSync root-sync, because RepoSync (nr2)
	// does not have permission to manage RBAC.
	nt.Must(repoSync2GitRepo.CommitAndPush("Adding RepoSync: " + repoSync4Key.String()))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr4.
	nt.Must(nt.WatchForAllSyncs(
		nomostest.SkipNonRootRepos(repoSync5Key)))

	nt.T.Logf("Add RepoSync %s to RootSync %s", repoSync5Key, rootSync1ID.Name)
	nrs5 := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSync5Key)
	// Ensure the RoleBinding is deleted after the RepoSync
	nt.Must(nomostest.SetDependencies(nrs5, nt.Scheme, nrb5))
	nt.Must(rootSync1GitRepo.Add(fmt.Sprintf("acme/reposyncs/%s.yaml", repoSync5Key.Name), nrs5))
	nt.Must(rootSync1GitRepo.Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, repoSync5Key.Name), nrb5))
	nt.Must(rootSync1GitRepo.CommitAndPush("Adding RepoSync: " + repoSync5Key.String()))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr5.
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Logf("Validate reconciler Deployment labels")
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{"app": "reconciler"}, 10)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 4)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNameLabel: rootSync1ID.Name}, 1)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNameLabel: repoSync1ID.Name}, 2)

	// Deployments may still be reconciling, wait before checking Pods
	nt.Must(waitForResourcesCurrent(nt, kinds.Deployment(), map[string]string{"app": "reconciler"}, 10))

	validateReconcilerResource(nt, kinds.Pod(), map[string]string{"app": "reconciler"}, 10)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 4)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNameLabel: rootSync1ID.Name}, 1)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNameLabel: repoSync1ID.Name}, 2)

	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 4)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNameLabel: rootSync1ID.Name}, 1)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNameLabel: repoSync1ID.Name}, 2)

	// Reconciler-manager doesn't copy the secret of RootSync's secretRef.
	validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 0)
	// CSR auth type doesn't need to copy the secret
	if nt.GitProvider.Type() != e2e.CSR {
		validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
		validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
		validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNameLabel: repoSync1ID.Name}, 2)
	}

	// TODO: validate sync-generation label
}

// resetExpectedGitSync creates, updates, or replaces the SyncSource for the
// specified RootSync or RepoSync with a new or reset Git repository.
func resetExpectedGitSync(nt *nomostest.NT, syncID core.ID) {
	if nt.GitProvider.Type() == e2e.Local {
		nomostest.InitGitRepos(nt, syncID.ObjectKey)
	}
	sourceFormat := configsync.SourceFormatUnstructured
	nomostest.SetExpectedSyncSource(nt, syncID, &syncsource.GitSyncSource{
		Repository:   nomostest.ResetRepository(nt, syncID.Kind, syncID.ObjectKey, sourceFormat),
		Branch:       gitproviders.MainBranch,
		SourceFormat: sourceFormat,
		Directory:    gitproviders.DefaultSyncDir,
	})
}

func validateReconcilerResource(nt *nomostest.NT, gvk schema.GroupVersionKind, labels map[string]string, expectedCount int) {
	list := kinds.NewUnstructuredListForItemGVK(gvk)
	if err := nt.KubeClient.List(list, client.MatchingLabels(labels)); err != nil {
		nt.T.Fatal(err)
	}
	if len(list.Items) != expectedCount {
		nt.T.Fatalf("expected %d reconciler %s(s), got %d:\n%s",
			expectedCount, gvk.Kind, len(list.Items), log.AsYAML(list))
	}
}

func waitForResourcesCurrent(nt *nomostest.NT, gvk schema.GroupVersionKind, labels map[string]string, expectedCount int) error {
	list := kinds.NewUnstructuredListForItemGVK(gvk)
	if err := nt.KubeClient.List(list, client.MatchingLabels(labels)); err != nil {
		return err
	}
	if len(list.Items) != expectedCount {
		return fmt.Errorf("expected %d reconciler %s(s), got %d",
			expectedCount, gvk.Kind, len(list.Items))
	}
	tg := taskgroup.New()
	for _, dep := range list.Items {
		nn := types.NamespacedName{Name: dep.GetName(), Namespace: dep.GetNamespace()}
		tg.Go(func() error {
			return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), nn.Name, nn.Namespace)
		})
	}
	return tg.Wait()
}

func TestConflictingDefinitions_RootToNamespace(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	repoSyncID := core.RepoSyncID("rs-test", testNs)
	rootSyncKey := rootSyncID.ObjectKey
	repoSyncKey := repoSyncID.ObjectKey
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.SyncWithGitSource(repoSyncID),
		ntopts.RepoSyncPermissions(policy.RBACAdmin()), // NS Reconciler manages Roles
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	repoSyncGitRepo := nt.SyncSourceGitReadWriteRepository(repoSyncID)

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to root: %s", rootSyncKey.Name)
	roleObj := rootPodRole()
	nt.Must(rootSyncGitRepo.Add(podRoleFilePath, roleObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("add pod viewer role"))
	nt.Must(nt.WatchForAllSyncs())

	// Add Role to the RootSync, NOT the RepoSync
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncKey, roleObj)

	err := nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Declare a conflicting Role in the Namespace repo: %s", repoSyncKey)
	nt.Must(repoSyncGitRepo.Add(podRoleFilePath, namespacePodRole()))
	nt.Must(repoSyncGitRepo.CommitAndPush("add conflicting pod owner role"))

	nt.T.Logf("The RootSync should report no problems")
	nt.Must(nt.WatchForAllSyncs(nomostest.RootSyncOnly()))

	nt.T.Logf("The RepoSync %s reports a problem since it can't sync the declaration.", repoSyncKey)
	nt.Must(nt.Watcher.WatchForRepoSyncSyncError(repoSyncKey.Namespace, repoSyncKey.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil))

	nt.T.Logf("Validate conflict metric is emitted from Namespace reconciler %s", repoSyncKey)
	repoSyncLabels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncKey)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := repoSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		// ManagementConflictErrorWrap is recorded by the remediator, while
		// KptManagementConflictError is recorded by the applier, but they have
		// similar error messages. So while there should be a ReconcilerError
		// metric, there might not be a LastSyncTimestamp with status=error.
		// nomostest.ReconcilerSyncError(nt, repoSyncLabels, commitHash),
		nomostest.ReconcilerErrorMetrics(nt, repoSyncLabels, commitHash, metrics.ErrorSummary{
			Conflicts: 1,
			Sync:      1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Ensure the Role matches the one in the Root repo %s", rootSyncKey.Name)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rootSyncKey.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the declaration from the Root repo %s", rootSyncKey.Name)
	nt.Must(rootSyncGitRepo.Remove(podRoleFilePath))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove conflicting pod role from Root"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Logf("Ensure the Role is updated to the one in the Namespace repo %s", repoSyncKey)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(namespacePodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSyncKey.Namespace), repoSyncKey.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete Role from the RootSync, and add it to the RepoSync.
	// RootSync will delete the object, because it was in the inventory, due to the AdoptAll strategy.
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncKey, roleObj)
	// RepoSync will recreate the object.
	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncKey, roleObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
		Errors: metrics.ErrorSummary{
			// resource_conflicts_total is cumulative and ony resets whe the commit changes
			// TODO: Fix resource_conflicts_total to reflect the actual current total number of conflicts.
			Conflicts: 1,
		},
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestConflictingDefinitions_NamespaceToRoot(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	repoSyncID := core.RepoSyncID("rs-test", testNs)
	rootSyncKey := rootSyncID.ObjectKey
	repoSyncKey := repoSyncID.ObjectKey
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.SyncWithGitSource(repoSyncID),
		ntopts.RepoSyncPermissions(policy.RBACAdmin()), // NS reconciler manages Roles
	)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	repoSyncGitRepo := nt.SyncSourceGitReadWriteRepository(repoSyncID)

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to Namespace repo: %s", rootSyncKey.Name)
	nsRoleObj := namespacePodRole()
	nt.Must(repoSyncGitRepo.Add(podRoleFilePath, nsRoleObj))
	nt.Must(repoSyncGitRepo.CommitAndPush("declare Role"))
	nt.Must(nt.WatchForAllSyncs())

	err := nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(nsRoleObj.Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSyncKey.Namespace), repoSyncKey.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Test Role managed by the RepoSync, NOT the RootSync
	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncKey, nsRoleObj)

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate no errors from namespace reconciler.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Declare a conflicting Role in the Root repo: %s", rootSyncKey.Name)
	rootRoleObj := rootPodRole()
	nt.Must(rootSyncGitRepo.Add(podRoleFilePath, rootRoleObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("add conflicting pod role to Root"))

	nt.T.Logf("The RootSync should update the Role")
	nt.Must(nt.WatchForAllSyncs(nomostest.RootSyncOnly()))

	nt.T.Log("The RepoSync remediator should report a conflict error")
	nt.Must(nt.Watcher.WatchForRepoSyncSyncError(repoSyncKey.Namespace, repoSyncKey.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncKey, rootRoleObj)

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Validate conflict metric is emitted from Namespace reconciler %s", repoSyncKey)
	repoSyncLabels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncKey)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := repoSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerSyncError(nt, repoSyncLabels, commitHash),
		nomostest.ReconcilerErrorMetrics(nt, repoSyncLabels, commitHash, metrics.ErrorSummary{
			Conflicts: 1,
			Sync:      1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Ensure the Role matches the one in the Root repo %s", rootSyncKey.Name)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rootSyncKey.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the Role from the Namespace repo %s", repoSyncKey)
	nt.Must(repoSyncGitRepo.Remove(podRoleFilePath))
	nt.Must(repoSyncGitRepo.CommitAndPush("remove conflicting pod role from Namespace repo"))
	// rs-test tries to delete the Role, but since it is also being managed by root-sync
	// it may Timeout waiting for the deletion to reconcile.
	nt.Must(nt.WatchForAllSyncs(nomostest.SkipAllResourceGroupChecks()))

	nt.T.Logf("Ensure the Role still matches the one in the Root repo %s", rootSyncKey.Name)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rootSyncKey.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Test Role managed by the RootSync, NOT the RepoSync
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncKey, rootRoleObj)
	// RepoSync won't delete the object, because it doesn't own it, due to the AdoptIfNoInventory strategy.
	nt.MetricsExpectations.RemoveObject(configsync.RepoSyncKind, repoSyncKey, nsRoleObj)

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate no errors from namespace reconciler.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestConflictingDefinitions_RootToRoot(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	rootSync2ID := core.RootSyncID("root-test")
	// If declaring RootSync in a Root repo, the source format has to be unstructured.
	// Otherwise, the hierarchical validator will complain that the config-management-system has configs but missing a Namespace config.
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.SyncWithGitSource(rootSync2ID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	rootSync2GitRepo := nt.SyncSourceGitReadWriteRepository(rootSync2ID)

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to RootSync: %s", rootSyncID.Name)
	nt.Must(rootSyncGitRepo.Add(podRoleFilePath, rootPodRole()))
	nt.Must(rootSyncGitRepo.CommitAndPush("add pod viewer role"))
	nt.Must(nt.WatchForAllSyncs())
	nt.T.Logf("Ensure the Role is managed by RootSync %s", rootSyncID.Name)
	role := &rbacv1.Role{}
	err := nt.Validate("pods", testNs, role,
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rootSyncID.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	roleResourceVersion := role.ResourceVersion

	nt.T.Logf("Declare a conflicting Role in RootSync %s", rootSync2ID.Name)
	nt.Must(rootSync2GitRepo.Add(podRoleFilePath, rootPodRole()))
	nt.Must(rootSync2GitRepo.CommitAndPush("add conflicting pod owner role"))

	// When the webhook is enabled, it will block adoption of managed objects.
	nt.T.Logf("Only RootSync %s should report a conflict with the webhook enabled", rootSync2ID.Name)
	tg := taskgroup.New()
	// The first reconciler never encounters any conflict.
	// The second reconciler pauses its remediator before applying, but then its
	// apply is rejected by the webhook, so the remediator remains paused.
	// So there's no need to report the error to the first reconciler.
	// So the first reconciler apply succeeds and no further error is expected.
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace)
	})
	// The second reconciler's applier will report the conflict, when the update
	// is rejected by the webhook.
	// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiserver/pkg/admission/plugin/webhook/errors/statuserror.go#L29
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync2ID.Name, rootSync2ID.Namespace,
			testwatcher.WatchPredicates(
				testpredicates.RootSyncHasSyncError(applier.ApplierErrorCode, "denied the request", nil),
			))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("The Role resource version should not be changed")
	if err := nt.Validate("pods", testNs, &rbacv1.Role{},
		testpredicates.ResourceVersionEquals(nt.Scheme, roleResourceVersion)); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Stop the admission webhook, to confirm that the remediator still reports the conflicts")
	nomostest.StopWebhook(nt)

	// When the webhook is disabled, both RootSyncs will repeatedly try to adopt
	// the object.
	// This error can be reported from two sources:
	// - `KptManagementConflictError` is returned by the applier
	// - `ManagementConflictErrorWrap` is returned by the remediator
	// Both use the phrase "detected a management conflict".
	nt.T.Logf("Both RootSyncs should still report conflicts with the webhook disabled")
	tg = taskgroup.New()
	// Watch the Role until it has been updated by both remediators
	manager1 := declared.ResourceManager(declared.RootScope, rootSyncID.Name)
	manager2 := declared.ResourceManager(declared.RootScope, rootSync2ID.Name)
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Role(), "pods", testNs,
			testwatcher.WatchPredicates(
				testpredicates.HasBeenManagedBy(nt.Scheme, nt.Logger, manager1, manager2),
			))
	})
	// Reconciler conflict, detected by the first reconciler's applier OR reported by the second reconciler
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
			testwatcher.WatchPredicates(
				testpredicates.RootSyncHasSyncError(status.ManagementConflictErrorCode, "detected a management conflict", nil),
			))
	})
	// Reconciler conflict, detected by the second reconciler's applier OR reported by the first reconciler
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync2ID.Name, rootSync2ID.Namespace,
			testwatcher.WatchPredicates(
				testpredicates.RootSyncHasSyncError(status.ManagementConflictErrorCode, "detected a management conflict", nil),
			))
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the declaration from RootSync %s", rootSyncID.Name)
	nt.Must(rootSyncGitRepo.Remove(podRoleFilePath))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove conflicting pod role"))
	// root-sync tries to delete the Role, but since it is also being managed by root-test
	// it may Timeout waiting for the deletion to reconcile.
	nt.Must(nt.WatchForAllSyncs(nomostest.SkipAllResourceGroupChecks()))

	nt.T.Logf("Ensure the Role is managed by RootSync %s", rootSync2ID.Name)
	// The pod role may be deleted from the cluster after it was removed from the `root-sync` Root repo.
	// Therefore, we need to retry here to wait until the `root-test` Root repo recreates the pod role.
	nt.Must(nt.Watcher.WatchObject(kinds.Role(), "pods", testNs,
		testwatcher.WatchPredicates(
			roleHasRules(rootPodRole().Rules),
			testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rootSync2ID.Name),
		)))
}

func TestConflictingDefinitions_NamespaceToNamespace(t *testing.T) {
	rootSyncKey := nomostest.DefaultRootSyncID.ObjectKey
	repoSync1ID := core.RepoSyncID("rs-test-1", testNs)
	repoSync2ID := core.RepoSyncID("rs-test-2", testNs)
	repoSync1Key := repoSync1ID.ObjectKey
	repoSync2Key := repoSync2ID.ObjectKey

	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.RepoSyncPermissions(policy.RBACAdmin()), // NS reconciler manages Roles
		ntopts.SyncWithGitSource(repoSync1ID),
		ntopts.SyncWithGitSource(repoSync2ID))

	repoSync1GitRepo := nt.SyncSourceGitReadWriteRepository(repoSync1ID)
	repoSync2GitRepo := nt.SyncSourceGitReadWriteRepository(repoSync2ID)

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to Namespace: %s", repoSync1Key)
	roleObj := namespacePodRole()
	nt.Must(repoSync1GitRepo.Add(podRoleFilePath, roleObj))
	nt.Must(repoSync1GitRepo.CommitAndPush("add pod viewer role"))
	nt.Must(nt.WatchForAllSyncs())
	role := &rbacv1.Role{}
	nt.T.Logf("Ensure the Role is managed by Namespace Repo %s", repoSync1Key)
	nt.Must(nt.Validate("pods", testNs, role,
		roleHasRules(roleObj.Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSync1Key.Namespace), repoSync1Key.Name)))
	roleResourceVersion := role.ResourceVersion

	// Validate no errors from root reconciler.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncKey,
		// RepoSync already included in the default resource count and operations
	}))

	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSync1Key, roleObj)

	// Validate no errors from namespace reconciler #1.
	nt.Must(nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSync1Key,
	}))

	// Validate no errors from namespace reconciler #2.
	nt.Must(nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSync2Key,
	}))

	nt.T.Logf("Declare a conflicting Role in another Namespace repo: %s", repoSync2Key)
	nt.Must(repoSync2GitRepo.Add(podRoleFilePath, roleObj))
	nt.Must(repoSync2GitRepo.CommitAndPush("add conflicting pod owner role"))

	nt.T.Logf("Only RepoSync %s reports the conflict error because kpt_applier won't update the resource", repoSync2Key)
	nt.Must(nt.Watcher.WatchForRepoSyncSyncError(repoSync2Key.Namespace, repoSync2Key.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil))
	nt.Must(nt.WatchForSync(kinds.RepoSyncV1Beta1(), repoSync1ID.Name, repoSync1ID.Namespace,
		nt.SyncSources[repoSync1ID]))
	nt.T.Logf("The Role resource version should not be changed")
	nt.Must(nt.Validate("pods", testNs, &rbacv1.Role{},
		testpredicates.ResourceVersionEquals(nt.Scheme, roleResourceVersion)))

	nt.T.Logf("Stop the admission webhook, the remediator should not be affected, which still reports the conflicts")
	nomostest.StopWebhook(nt)
	nt.Must(nt.Watcher.WatchForRepoSyncSyncError(repoSync2Key.Namespace, repoSync2Key.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil))
	nt.Must(nt.WatchForSync(kinds.RepoSyncV1Beta1(), repoSync1ID.Name, repoSync1ID.Namespace,
		nt.SyncSources[repoSync1ID]))
	nt.T.Logf("Validate conflict metric is emitted from Namespace reconciler %s", repoSync2Key)
	repoSync2Labels, err := nomostest.MetricLabelsForRepoSync(nt, repoSync2Key)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := repoSync2GitRepo.MustHash(nt.T)

	nt.Must(nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerSyncError(nt, repoSync2Labels, commitHash),
		nomostest.ReconcilerErrorMetrics(nt, repoSync2Labels, commitHash, metrics.ErrorSummary{
			Conflicts: 1,
			Sync:      1,
		})))

	nt.T.Logf("Remove the declaration from one Namespace repo %s", repoSync1Key)
	nt.Must(repoSync1GitRepo.Remove(podRoleFilePath))
	nt.Must(repoSync1GitRepo.CommitAndPush("remove conflicting pod role from Namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Logf("Ensure the Role is managed by the other Namespace repo %s", repoSync2Key)
	nt.Must(nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(roleObj.Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSync2Key.Namespace), repoSync2Key.Name)))

	// Validate no errors from root reconciler.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncKey,
		// RepoSync already included in the default resource count and operations
	}))

	nt.MetricsExpectations.AddObjectDelete(configsync.RepoSyncKind, repoSync1Key, roleObj)

	// Validate no errors from namespace reconciler #1.
	nt.Must(nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSync1Key,
	}))

	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSync2Key, roleObj)

	// Validate no errors from namespace reconciler #2.
	nt.Must(nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSync2Key,
		Errors: metrics.ErrorSummary{
			// resource_conflicts_total is cumulative and ony resets whe the commit changes
			// TODO: Fix resource_conflicts_total to reflect the actual current total number of conflicts.
			Conflicts: 1,
		},
	}))
}

func TestControllerValidationErrors(t *testing.T) {
	nt := nomostest.New(t, nomostesting.MultiRepos)

	testNamespace := k8sobjects.NamespaceObject(testNs)
	nt.Must(nt.KubeClient.Create(testNamespace))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, testNamespace))
	})

	nt.T.Logf("Validate RootSync can only exist in the config-management-system namespace")
	rootSync := k8sobjects.RootSyncObjectV1Beta1("rs-test", core.Namespace(testNs))
	rootSync.Spec.Git = &v1beta1.Git{Auth: configsync.AuthNone}
	nomostest.SetRSyncTestDefaults(nt, rootSync)
	nt.Must(nt.KubeClient.Create(rootSync))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, rootSync))
	})
	// Can't use WaitForRootSyncStalledError because it doesn't take other namespaces
	expectedCondition := &v1beta1.RootSyncCondition{
		Type:    v1beta1.RootSyncStalled,
		Status:  metav1.ConditionTrue,
		Reason:  "Validation",
		Message: "RootSync objects are only allowed in the config-management-system namespace, not in test-ns",
		ErrorSummary: &v1beta1.ErrorSummary{
			ErrorCountAfterTruncation: 1,
			TotalCount:                1,
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.Name, rootSync.Namespace,
		testwatcher.WatchPredicates(testpredicates.RootSyncHasCondition(expectedCondition))))

	nt.T.Logf("Validate RepoSync is not allowed in the config-management-system namespace")
	rsControllerNamespace := k8sobjects.RepoSyncObjectV1Beta1(configsync.ControllerNamespace, configsync.RepoSyncName)
	rsControllerNamespace.Spec.Git = &v1beta1.Git{Auth: configsync.AuthNone}
	nomostest.SetRSyncTestDefaults(nt, rsControllerNamespace)
	nt.Must(nt.KubeClient.Create(rsControllerNamespace))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, rsControllerNamespace))
	})
	nt.WaitForRepoSyncStalledError(rsControllerNamespace.Namespace, rsControllerNamespace.Name, "Validation",
		"RepoSync objects are not allowed in the config-management-system namespace")

	nt.T.Logf("Validate an invalid config with a long RepoSync name")
	longBytes := make([]byte, validation.DNS1123SubdomainMaxLength)
	for i := range longBytes {
		longBytes[i] = 'a'
	}
	veryLongName := string(longBytes)
	rsTooLong := k8sobjects.RepoSyncObjectV1Beta1(testNs, veryLongName)
	rsTooLong.Spec.Git = &v1beta1.Git{Auth: configsync.AuthNone}
	nomostest.SetRSyncTestDefaults(nt, rsTooLong)
	nt.Must(nt.KubeClient.Create(rsTooLong))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, rsTooLong))
	})
	nt.WaitForRepoSyncStalledError(rsTooLong.Namespace, rsTooLong.Name, "Validation",
		`maximum combined length of RepoSync name and namespace is 45, but found 260`)

	nt.T.Logf("Validate an invalid config with a long RepoSync Secret name")
	rsInvalidSecretRef := k8sobjects.RepoSyncObjectV1Beta1(testNs, "repo-test")
	rsInvalidSecretRef.Spec.Git = &v1beta1.Git{
		// Repo required to avoid error:
		// KNV1061: RepoSyncs must specify spec.git.repo when spec.sourceType is "git"
		Repo:      "https://github.com/test/test",
		Auth:      configsync.AuthSSH,
		SecretRef: &v1beta1.SecretReference{Name: veryLongName},
	}
	nomostest.SetRSyncTestDefaults(nt, rsInvalidSecretRef)
	nt.Must(nt.KubeClient.Create(rsInvalidSecretRef))
	t.Cleanup(func() {
		nt.Must(nomostest.DeleteObjectsAndWait(nt, rsInvalidSecretRef))
	})
	nt.WaitForRepoSyncStalledError(rsInvalidSecretRef.Namespace, rsInvalidSecretRef.Name, "Validation",
		fmt.Sprintf(`The managed secret name "%s-%s" is invalid: must be no more than %d characters. To fix it, update '.spec.git.secretRef.name'`,
			core.NsReconcilerName(rsInvalidSecretRef.Namespace, rsInvalidSecretRef.Name), veryLongName, validation.DNS1123SubdomainMaxLength))
}

func rootPodRole() *rbacv1.Role {
	result := k8sobjects.RoleObject(
		core.Name("pods"),
		core.Namespace(testNs),
	)
	result.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{corev1.GroupName},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "list"},
		},
	}
	return result
}

func namespacePodRole() *rbacv1.Role {
	result := k8sobjects.RoleObject(
		core.Name("pods"),
		core.Namespace(testNs),
	)
	result.Rules = []rbacv1.PolicyRule{
		{
			APIGroups: []string{corev1.GroupName},
			Resources: []string{"pods"},
			Verbs:     []string{"*"},
		},
	}
	return result
}

func roleHasRules(wantRules []rbacv1.PolicyRule) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		r, isRole := o.(*rbacv1.Role)
		if !isRole {
			return testpredicates.WrongTypeErr(o, &rbacv1.Role{})
		}

		if diff := cmp.Diff(wantRules, r.Rules); diff != "" {
			return fmt.Errorf("Pod Role .rules diff: %s", diff)
		}
		return nil
	}
}
