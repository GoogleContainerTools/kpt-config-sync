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
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
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
// - root-sync is created using k8s api.
// - rr1 is created using k8s api. This is to validate multiple RootSyncs can be created in the delegated mode.
// - rr2 is a v1alpha1 version of RootSync declared in the root repo of root-sync. This is to validate RootSync can be managed in a root repo and validate the v1alpha1 version API.
// - rr3 is a v1alpha1 version of RootSync declared in rs-2. This is to validate RootSync can be managed in a different root repo and validate the v1alpha1 version API.
// - nr1 is created using k8s api. This is to validate RepoSyncs can be created in the delegated mode.
// - nr2 is a v1alpha1 version of RepoSync created using k8s api. This is to validate v1alpha1 version of RepoSync can be created in the delegated mode.
// - nr3 is declared in the root repo of root-sync. This is to validate RepoSync can be managed in a root repo.
// - nr4 is a v1alpha1 version of RepoSync declared in the namespace repo of nn2. This is to validate RepoSync can be managed in a namespace repo in the same namespace.
// - nr5 is declared in the root repo of rr1. This is to validate implicit namespace won't cause conflict between two root reconcilers (rr1 and root-sync).
// - nr6 is created using k8s api in a different namespace but with the same name "nr1".
func TestMultiSyncs_Unstructured_MixedControl(t *testing.T) {
	rr1 := "rr1"
	rr2 := "rr2"
	rr3 := "rr3"
	nr1 := "nr1"
	nn1 := nomostest.RepoSyncNN(testNs, nr1)
	nn2 := nomostest.RepoSyncNN(testNs, "nr2")
	nn3 := nomostest.RepoSyncNN(testNs, "nr3")
	nn4 := nomostest.RepoSyncNN(testNs, "nr4")
	nn5 := nomostest.RepoSyncNN(testNs, "nr5")
	testNs2 := "ns-2"
	nn6 := nomostest.RepoSyncNN(testNs2, nr1)

	nt := nomostest.New(t, nomostesting.MultiRepos, ntopts.Unstructured,
		ntopts.WithDelegatedControl, ntopts.RootRepo(rr1),
		// NS reconciler allowed to manage RepoSyncs but not RoleBindings
		ntopts.RepoSyncPermissions(policy.RepoSyncAdmin()),
		ntopts.NamespaceRepo(nn1.Namespace, nn1.Name),
		ntopts.NamespaceRepo(nn6.Namespace, nn6.Name))

	// Cleanup all unmanaged RepoSyncs BEFORE the root-sync is deleted!
	// Otherwise, the test Namespace will be deleted while still containing
	// RepoSyncs, which could block deletion if their finalizer hangs.
	// This also replaces depends-on deletion ordering (RoleBinding -> RepoSync),
	// which can't be used by unmanaged syncs or objects in different repos.
	nt.T.Cleanup(func() {
		nt.T.Log("[CLEANUP] Deleting test RepoSyncs")
		var rsList []v1beta1.RepoSync
		rsNNs := []types.NamespacedName{nn1, nn2, nn4}
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

	var newRepos []types.NamespacedName
	newRepos = append(newRepos, nomostest.RootSyncNN(rr2))
	newRepos = append(newRepos, nomostest.RootSyncNN(rr3))
	newRepos = append(newRepos, nn2)
	newRepos = append(newRepos, nn3)
	newRepos = append(newRepos, nn4)
	newRepos = append(newRepos, nn5)

	if nt.GitProvider.Type() == e2e.Local {
		nomostest.InitGitRepos(nt, newRepos...)
	}
	nt.RootRepos[rr2] = nomostest.ResetRepository(nt, gitproviders.RootRepo, nomostest.RootSyncNN(rr2), configsync.SourceFormatUnstructured)
	nt.RootRepos[rr3] = nomostest.ResetRepository(nt, gitproviders.RootRepo, nomostest.RootSyncNN(rr3), configsync.SourceFormatUnstructured)
	nt.NonRootRepos[nn2] = nomostest.ResetRepository(nt, gitproviders.NamespaceRepo, nn2, configsync.SourceFormatUnstructured)
	nt.NonRootRepos[nn3] = nomostest.ResetRepository(nt, gitproviders.NamespaceRepo, nn3, configsync.SourceFormatUnstructured)
	nt.NonRootRepos[nn4] = nomostest.ResetRepository(nt, gitproviders.NamespaceRepo, nn4, configsync.SourceFormatUnstructured)
	nt.NonRootRepos[nn5] = nomostest.ResetRepository(nt, gitproviders.NamespaceRepo, nn5, configsync.SourceFormatUnstructured)

	nrb2 := nomostest.RepoSyncRoleBinding(nn2)
	nrb3 := nomostest.RepoSyncRoleBinding(nn3)
	nrb4 := nomostest.RepoSyncRoleBinding(nn4)
	nrb5 := nomostest.RepoSyncRoleBinding(nn5)

	nt.T.Logf("Adding Namespace & RoleBindings for RepoSyncs")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/cluster/ns-%s.yaml", testNs), k8sobjects.NamespaceObject(testNs)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, nn2.Name), nrb2))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, nn4.Name), nrb4))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Namespace & RoleBindings for RepoSyncs"))

	nt.T.Logf("Add RootSync %s to the repository of RootSync %s", rr2, configsync.RootSyncName)

	rs2 := nomostest.RootSyncObjectV1Alpha1FromRootRepo(nt, rr2)
	rs2ConfigFile := fmt.Sprintf("acme/rootsyncs/%s.yaml", rr2)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(rs2ConfigFile, rs2))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding RootSync: " + rr2))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RootSync rr2.
	if err := nt.WatchForAllSyncs(nomostest.SkipRootRepos(rr3), nomostest.SkipNonRootRepos(nn2, nn3, nn4, nn5)); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Add RootSync %s to the repository of RootSync %s", rr3, rr2)
	rs3 := nomostest.RootSyncObjectV1Alpha1FromRootRepo(nt, rr3)
	rs3ConfigFile := fmt.Sprintf("acme/rootsyncs/%s.yaml", rr3)
	nt.Must(nt.RootRepos[rr2].Add(rs3ConfigFile, rs3))
	nt.Must(nt.RootRepos[rr2].CommitAndPush("Adding RootSync: " + rr3))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RootSync rr3.
	if err := nt.WatchForAllSyncs(nomostest.SkipNonRootRepos(nn2, nn3, nn4, nn5)); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Create RepoSync %s", nn2)
	nrs2 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn2)
	if err := nt.KubeClient.Create(nrs2); err != nil {
		nt.T.Fatal(err)
	}
	// RoleBinding (nrb2) managed by RootSync root-sync, because the namespace
	// tenant does not have permission to manage RBAC.
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr2.
	if err := nt.WatchForAllSyncs(nomostest.SkipNonRootRepos(nn3, nn4, nn5)); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Add RepoSync %s to RootSync %s", nn3, configsync.RootSyncName)
	nrs3 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn3)
	// Ensure the RoleBinding is deleted after the RepoSync
	if err := nomostest.SetDependencies(nrs3, nrb3); err != nil {
		nt.T.Fatal(err)
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/reposyncs/%s.yaml", nn3.Name), nrs3))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, nn3.Name), nrb3))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding RepoSync: " + nn3.String()))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr3.
	if err := nt.WatchForAllSyncs(nomostest.SkipNonRootRepos(nn4, nn5)); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Add RepoSync %s to RepoSync %s", nn4, nn2)
	nrs4 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn4)
	nt.Must(nt.NonRootRepos[nn2].Add(fmt.Sprintf("acme/reposyncs/%s.yaml", nn4.Name), nrs4))
	// RoleBinding (nrb4) managed by RootSync root-sync, because RepoSync (nr2)
	// does not have permission to manage RBAC.
	nt.Must(nt.NonRootRepos[nn2].CommitAndPush("Adding RepoSync: " + nn4.String()))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr4.
	if err := nt.WatchForAllSyncs(nomostest.SkipNonRootRepos(nn5)); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Add RepoSync %s to RootSync %s", nn5, rr1)
	nrs5 := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn5)
	// Ensure the RoleBinding is deleted after the RepoSync
	if err := nomostest.SetDependencies(nrs5, nrb5); err != nil {
		nt.T.Fatal(err)
	}
	nt.Must(nt.RootRepos[rr1].Add(fmt.Sprintf("acme/reposyncs/%s.yaml", nn5.Name), nrs5))
	nt.Must(nt.RootRepos[rr1].Add(fmt.Sprintf("acme/namespaces/%s/rb-%s.yaml", testNs, nn5.Name), nrb5))
	nt.Must(nt.RootRepos[rr1].CommitAndPush("Adding RepoSync: " + nn5.String()))
	// Wait for all RootSyncs and RepoSyncs to be synced, including the new RepoSync nr5.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Validate reconciler Deployment labels")
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{"app": "reconciler"}, 10)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 4)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNameLabel: rr1}, 1)
	validateReconcilerResource(nt, kinds.Deployment(), map[string]string{metadata.SyncNameLabel: nr1}, 2)

	// Deployments may still be reconciling, wait before checking Pods
	if err := waitForResourcesCurrent(nt, kinds.Deployment(), map[string]string{"app": "reconciler"}, 10); err != nil {
		nt.T.Fatal(err)
	}
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{"app": "reconciler"}, 10)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 4)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNameLabel: rr1}, 1)
	validateReconcilerResource(nt, kinds.Pod(), map[string]string{metadata.SyncNameLabel: nr1}, 2)

	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 4)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNameLabel: rr1}, 1)
	validateReconcilerResource(nt, kinds.ServiceAccount(), map[string]string{metadata.SyncNameLabel: nr1}, 2)

	// Reconciler-manager doesn't copy the secret of RootSync's secretRef.
	validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNamespaceLabel: configsync.ControllerNamespace}, 0)
	// CSR auth type doesn't need to copy the secret
	if nt.GitProvider.Type() != e2e.CSR {
		validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNamespaceLabel: testNs}, 5)
		validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNamespaceLabel: testNs2}, 1)
		validateReconcilerResource(nt, kinds.Secret(), map[string]string{metadata.SyncNameLabel: nr1}, 2)
	}

	// TODO: validate sync-generation label
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
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	repoSyncNN := nomostest.RepoSyncNN(testNs, "rs-test")
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name),
		ntopts.RepoSyncPermissions(policy.RBACAdmin()), // NS Reconciler manages Roles
	)

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to root: %s", configsync.RootSyncName)
	roleObj := rootPodRole()
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(podRoleFilePath, roleObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add pod viewer role"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Add Role to the RootSync, NOT the RepoSync
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, roleObj)

	err := nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Declare a conflicting Role in the Namespace repo: %s", repoSyncNN)
	nt.Must(nt.NonRootRepos[repoSyncNN].Add(podRoleFilePath, namespacePodRole()))
	nt.Must(nt.NonRootRepos[repoSyncNN].CommitAndPush("add conflicting pod owner role"))

	nt.T.Logf("The RootSync should report no problems")
	err = nt.WatchForAllSyncs(nomostest.RootSyncOnly())
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("The RepoSync %s reports a problem since it can't sync the declaration.", repoSyncNN)
	nt.WaitForRepoSyncSyncError(repoSyncNN.Namespace, repoSyncNN.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil)

	nt.T.Logf("Validate conflict metric is emitted from Namespace reconciler %s", repoSyncNN)
	repoSyncLabels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.NonRootRepos[repoSyncNN].MustHash(nt.T)

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

	nt.T.Logf("Ensure the Role matches the one in the Root repo %s", configsync.RootSyncName)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, configsync.RootSyncName))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the declaration from the Root repo %s", configsync.RootSyncName)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(podRoleFilePath))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove conflicting pod role from Root"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Ensure the Role is updated to the one in the Namespace repo %s", repoSyncNN)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(namespacePodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSyncNN.Namespace), repoSyncNN.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete Role from the RootSync, and add it to the RepoSync.
	// RootSync will delete the object, because it was in the inventory, due to the AdoptAll strategy.
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, roleObj)
	// RepoSync will recreate the object.
	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncNN, roleObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN,
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
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	repoSyncNN := nomostest.RepoSyncNN(testNs, "rs-test")
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name),
		ntopts.RepoSyncPermissions(policy.RBACAdmin()), // NS reconciler manages Roles
	)

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to Namespace repo: %s", configsync.RootSyncName)
	nsRoleObj := namespacePodRole()
	nt.Must(nt.NonRootRepos[repoSyncNN].Add(podRoleFilePath, nsRoleObj))
	nt.Must(nt.NonRootRepos[repoSyncNN].CommitAndPush("declare Role"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(nsRoleObj.Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSyncNN.Namespace), repoSyncNN.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Test Role managed by the RepoSync, NOT the RootSync
	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncNN, nsRoleObj)

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate no errors from namespace reconciler.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Declare a conflicting Role in the Root repo: %s", configsync.RootSyncName)
	rootRoleObj := rootPodRole()
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(podRoleFilePath, rootRoleObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add conflicting pod role to Root"))

	nt.T.Logf("The RootSync should update the Role")
	err = nt.WatchForAllSyncs(nomostest.RootSyncOnly())
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("The RepoSync remediator should report a conflict error")
	nt.WaitForRepoSyncSyncError(repoSyncNN.Namespace, repoSyncNN.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil)

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rootRoleObj)

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Validate conflict metric is emitted from Namespace reconciler %s", repoSyncNN)
	repoSyncLabels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.NonRootRepos[repoSyncNN].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerSyncError(nt, repoSyncLabels, commitHash),
		nomostest.ReconcilerErrorMetrics(nt, repoSyncLabels, commitHash, metrics.ErrorSummary{
			Conflicts: 1,
			Sync:      1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Ensure the Role matches the one in the Root repo %s", configsync.RootSyncName)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, configsync.RootSyncName))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the Role from the Namespace repo %s", repoSyncNN)
	nt.Must(nt.NonRootRepos[repoSyncNN].Remove(podRoleFilePath))
	nt.Must(nt.NonRootRepos[repoSyncNN].CommitAndPush("remove conflicting pod role from Namespace repo"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Ensure the Role still matches the one in the Root repo %s", configsync.RootSyncName)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, configsync.RootSyncName))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Test Role managed by the RootSync, NOT the RepoSync
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rootRoleObj)
	// RepoSync won't delete the object, because it doesn't own it, due to the AdoptIfNoInventory strategy.
	nt.MetricsExpectations.RemoveObject(configsync.RepoSyncKind, repoSyncNN, nsRoleObj)

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate no errors from namespace reconciler.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestConflictingDefinitions_RootToRoot(t *testing.T) {
	rootSync2 := "root-test"
	// If declaring RootSync in a Root repo, the source format has to be unstructured.
	// Otherwise, the hierarchical validator will complain that the config-management-system has configs but missing a Namespace config.
	nt := nomostest.New(t, nomostesting.MultiRepos, ntopts.Unstructured, ntopts.RootRepo(rootSync2))

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to RootSync: %s", configsync.RootSyncName)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(podRoleFilePath, rootPodRole()))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add pod viewer role"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Logf("Ensure the Role is managed by RootSync %s", configsync.RootSyncName)
	role := &rbacv1.Role{}
	err := nt.Validate("pods", testNs, role,
		roleHasRules(rootPodRole().Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, configsync.RootSyncName))
	if err != nil {
		nt.T.Fatal(err)
	}

	roleResourceVersion := role.ResourceVersion

	nt.T.Logf("Declare a conflicting Role in RootSync %s", rootSync2)
	nt.Must(nt.RootRepos[rootSync2].Add(podRoleFilePath, rootPodRole()))
	nt.Must(nt.RootRepos[rootSync2].CommitAndPush("add conflicting pod owner role"))

	// When the webhook is enabled, it will block adoption of managed objects.
	nt.T.Logf("Only RootSync %s should report a conflict with the webhook enabled", rootSync2)
	tg := taskgroup.New()
	// The first reconciler never encounters any conflict.
	// The second reconciler pauses its remediator before applying, but then its
	// apply is rejected by the webhook, so the remediator remains paused.
	// So there's no need to report the error to the first reconciler.
	// So the first reconciler apply succeeds and no further error is expected.
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace)
	})
	// The second reconciler's applier will report the conflict, when the update
	// is rejected by the webhook.
	// https://github.com/kubernetes/kubernetes/blob/master/staging/src/k8s.io/apiserver/pkg/admission/plugin/webhook/errors/statuserror.go#L29
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync2, configsync.ControllerNamespace,
			[]testpredicates.Predicate{
				testpredicates.RootSyncHasSyncError(applier.ApplierErrorCode, "denied the request"),
			})
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
	manager1 := declared.ResourceManager(declared.RootScope, configsync.RootSyncName)
	manager2 := declared.ResourceManager(declared.RootScope, rootSync2)
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.Role(), "pods", testNs,
			[]testpredicates.Predicate{
				testpredicates.HasBeenManagedBy(nt.Scheme, nt.Logger, manager1, manager2),
			})
	})
	// Reconciler conflict, detected by the first reconciler's applier OR reported by the second reconciler
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
			[]testpredicates.Predicate{
				testpredicates.RootSyncHasSyncError(status.ManagementConflictErrorCode, "detected a management conflict"),
			})
	})
	// Reconciler conflict, detected by the second reconciler's applier OR reported by the first reconciler
	tg.Go(func() error {
		return nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync2, configsync.ControllerNamespace,
			[]testpredicates.Predicate{
				testpredicates.RootSyncHasSyncError(status.ManagementConflictErrorCode, "detected a management conflict"),
			})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the declaration from RootSync %s", configsync.RootSyncName)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(podRoleFilePath))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove conflicting pod role"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Ensure the Role is managed by RootSync %s", rootSync2)
	// The pod role may be deleted from the cluster after it was removed from the `root-sync` Root repo.
	// Therefore, we need to retry here to wait until the `root-test` Root repo recreates the pod role.
	err = nt.Watcher.WatchObject(kinds.Role(), "pods", testNs,
		[]testpredicates.Predicate{
			roleHasRules(rootPodRole().Rules),
			testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rootSync2),
		})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestConflictingDefinitions_NamespaceToNamespace(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	repoSyncNN1 := nomostest.RepoSyncNN(testNs, "rs-test-1")
	repoSyncNN2 := nomostest.RepoSyncNN(testNs, "rs-test-2")

	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.RepoSyncPermissions(policy.RBACAdmin()), // NS reconciler manages Roles
		ntopts.NamespaceRepo(repoSyncNN1.Namespace, repoSyncNN1.Name),
		ntopts.NamespaceRepo(repoSyncNN2.Namespace, repoSyncNN2.Name))

	podRoleFilePath := fmt.Sprintf("acme/namespaces/%s/pod-role.yaml", testNs)
	nt.T.Logf("Add a Role to Namespace: %s", repoSyncNN1)
	roleObj := namespacePodRole()
	nt.Must(nt.NonRootRepos[repoSyncNN1].Add(podRoleFilePath, roleObj))
	nt.Must(nt.NonRootRepos[repoSyncNN1].CommitAndPush("add pod viewer role"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	role := &rbacv1.Role{}
	nt.T.Logf("Ensure the Role is managed by Namespace Repo %s", repoSyncNN1)
	err := nt.Validate("pods", testNs, role,
		roleHasRules(roleObj.Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSyncNN1.Namespace), repoSyncNN1.Name))
	if err != nil {
		nt.T.Fatal(err)
	}
	roleResourceVersion := role.ResourceVersion

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
		// RepoSync already included in the default resource count and operations
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncNN1, roleObj)

	// Validate no errors from namespace reconciler #1.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN1,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate no errors from namespace reconciler #2.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN2,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Declare a conflicting Role in another Namespace repo: %s", repoSyncNN2)
	nt.Must(nt.NonRootRepos[repoSyncNN2].Add(podRoleFilePath, roleObj))
	nt.Must(nt.NonRootRepos[repoSyncNN2].CommitAndPush("add conflicting pod owner role"))

	nt.T.Logf("Only RepoSync %s reports the conflict error because kpt_applier won't update the resource", repoSyncNN2)
	nt.WaitForRepoSyncSyncError(repoSyncNN2.Namespace, repoSyncNN2.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil)
	err = nt.WatchForSync(kinds.RepoSyncV1Beta1(), repoSyncNN1.Name, repoSyncNN1.Namespace,
		nomostest.DefaultRepoSha1Fn, nomostest.RepoSyncHasStatusSyncCommit, nil)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Logf("The Role resource version should not be changed")
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		testpredicates.ResourceVersionEquals(nt.Scheme, roleResourceVersion))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Stop the admission webhook, the remediator should not be affected, which still reports the conflicts")
	nomostest.StopWebhook(nt)
	nt.WaitForRepoSyncSyncError(repoSyncNN2.Namespace, repoSyncNN2.Name, status.ManagementConflictErrorCode, "detected a management conflict", nil)
	err = nt.WatchForSync(kinds.RepoSyncV1Beta1(), repoSyncNN1.Name, repoSyncNN1.Namespace,
		nomostest.DefaultRepoSha1Fn, nomostest.RepoSyncHasStatusSyncCommit, nil)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Logf("Validate conflict metric is emitted from Namespace reconciler %s", repoSyncNN2)
	repoSync2Labels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncNN2)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.NonRootRepos[repoSyncNN2].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerSyncError(nt, repoSync2Labels, commitHash),
		nomostest.ReconcilerErrorMetrics(nt, repoSync2Labels, commitHash, metrics.ErrorSummary{
			Conflicts: 1,
			Sync:      1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the declaration from one Namespace repo %s", repoSyncNN1)
	nt.Must(nt.NonRootRepos[repoSyncNN1].Remove(podRoleFilePath))
	nt.Must(nt.NonRootRepos[repoSyncNN1].CommitAndPush("remove conflicting pod role from Namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Ensure the Role is managed by the other Namespace repo %s", repoSyncNN2)
	err = nt.Validate("pods", testNs, &rbacv1.Role{},
		roleHasRules(roleObj.Rules),
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSyncNN2.Namespace), repoSyncNN2.Name))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate no errors from root reconciler.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
		// RepoSync already included in the default resource count and operations
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectDelete(configsync.RepoSyncKind, repoSyncNN1, roleObj)

	// Validate no errors from namespace reconciler #1.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN1,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncNN2, roleObj)

	// Validate no errors from namespace reconciler #2.
	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncNN2,
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

func TestControllerValidationErrors(t *testing.T) {
	nt := nomostest.New(t, nomostesting.MultiRepos)

	testNamespace := k8sobjects.NamespaceObject(testNs)
	if err := nt.KubeClient.Create(testNamespace); err != nil {
		nt.T.Fatal(err)
	}
	t.Cleanup(func() {
		if err := nomostest.DeleteObjectsAndWait(nt, testNamespace); err != nil {
			nt.T.Fatal(err)
		}
	})

	nt.T.Logf("Validate RootSync can only exist in the config-management-system namespace")
	rootSync := &v1beta1.RootSync{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rs-test",
			Namespace: testNs,
		},
		Spec: v1beta1.RootSyncSpec{
			Git: &v1beta1.Git{
				Auth: "none",
			},
		},
	}
	if err := nt.KubeClient.Create(rootSync); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRootSyncStalledError(rootSync.Namespace, rootSync.Name, "Validation", "RootSync objects are only allowed in the config-management-system namespace, not in test-ns")
	t.Cleanup(func() {
		if err := nomostest.DeleteObjectsAndWait(nt, rootSync); err != nil {
			nt.T.Fatal(err)
		}
	})

	nt.T.Logf("Validate RepoSync is not allowed in the config-management-system namespace")
	nnControllerNamespace := nomostest.RepoSyncNN(configsync.ControllerNamespace, configsync.RepoSyncName)
	rs := nomostest.RepoSyncObjectV1Beta1(nnControllerNamespace, "", configsync.SourceFormatUnstructured)
	if err := nt.KubeClient.Create(rs); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "RepoSync objects are not allowed in the config-management-system namespace")
	if err := nomostest.DeleteObjectsAndWait(nt, rs); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Validate an invalid config with a long RepoSync name")
	longBytes := make([]byte, validation.DNS1123SubdomainMaxLength)
	for i := range longBytes {
		longBytes[i] = 'a'
	}
	veryLongName := string(longBytes)
	nnTooLong := nomostest.RepoSyncNN(testNs, veryLongName)
	rs = nomostest.RepoSyncObjectV1Beta1(nnTooLong, "https://github.com/test/test", configsync.SourceFormatUnstructured)
	if err := nt.KubeClient.Create(rs); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation",
		fmt.Sprintf(`Invalid reconciler name "ns-reconciler-%s-%s-%d": must be no more than %d characters.`,
			testNs, veryLongName, len(veryLongName), validation.DNS1123SubdomainMaxLength))
	t.Cleanup(func() {
		if err := nomostest.DeleteObjectsAndWait(nt, rs); err != nil {
			nt.T.Fatal(err)
		}
	})

	nt.T.Logf("Validate an invalid config with a long RepoSync Secret name")
	nnInvalidSecretRef := nomostest.RepoSyncNN(testNs, "repo-test")
	rsInvalidSecretRef := nomostest.RepoSyncObjectV1Beta1(nnInvalidSecretRef, "https://github.com/test/test", configsync.SourceFormatUnstructured)
	rsInvalidSecretRef.Spec.Auth = configsync.AuthSSH
	rsInvalidSecretRef.Spec.SecretRef = &v1beta1.SecretReference{Name: veryLongName}
	if err := nt.KubeClient.Create(rsInvalidSecretRef); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncStalledError(rsInvalidSecretRef.Namespace, rsInvalidSecretRef.Name, "Validation",
		fmt.Sprintf(`The managed secret name "ns-reconciler-%s-%s-%d-%s" is invalid: must be no more than %d characters. To fix it, update '.spec.git.secretRef.name'`,
			testNs, rsInvalidSecretRef.Name, len(rsInvalidSecretRef.Name), v1beta1.GetSecretName(rsInvalidSecretRef.Spec.SecretRef), validation.DNS1123SubdomainMaxLength))
	t.Cleanup(func() {
		if err := nomostest.DeleteObjectsAndWait(nt, rsInvalidSecretRef); err != nil {
			nt.T.Fatal(err)
		}
	})
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
