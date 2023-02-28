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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNamespaceRepo_Centralized(t *testing.T) {
	bsNamespace := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
		ntopts.WithCentralizedControl,
	)

	// Validate status condition "Reconciling" and "Stalled "is set to "False"
	// after the reconciler deployment is successfully created.
	// RepoSync status conditions "Reconciling" and "Stalled" are derived from
	// namespace reconciler deployment.
	// Log error if the Reconciling condition does not progress to False before
	// the timeout expires.
	err := nomostest.WatchObject(nt, kinds.RepoSyncV1Beta1(), "repo-sync", bsNamespace,
		[]nomostest.Predicate{
			hasReconcilingStatus(metav1.ConditionFalse),
			hasStalledStatus(metav1.ConditionFalse),
		},
		nomostest.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Errorf("RepoSync did not finish reconciling: %v", err)
	}

	nn := nomostest.RepoSyncNN(bsNamespace, configsync.RepoSyncName)
	repo, exist := nt.NonRootRepos[nn]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}

	// Validate service account 'store' not present.
	err = nt.ValidateNotFound("store", bsNamespace, &corev1.ServiceAccount{})
	if err != nil {
		nt.T.Errorf("store service account already present: %v", err)
	}

	sa := fake.ServiceAccountObject("store", core.Namespace(bsNamespace))
	repo.Add("acme/sa.yaml", sa)
	repo.CommitAndPush("Adding service account")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Validate service account 'store' is current.
	err = nomostest.WatchForCurrentStatus(nt, kinds.ServiceAccount(), "store", bsNamespace,
		nomostest.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatalf("service account store not found: %v", err)
	}

	// Validate multi-repo metrics from namespace reconciler.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(core.NsReconcilerName(bsNamespace, configsync.RepoSyncName),
			1, // 1 for the test ServiceAccount
			metrics.ResourceCreated("ServiceAccount"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func hasReconcilingStatus(r metav1.ConditionStatus) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		rs := o.(*v1beta1.RepoSync)
		conditions := rs.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == v1beta1.RepoSyncReconciling && condition.Status != r {
				return fmt.Errorf("object %q has %q condition status %q; want %q", o.GetName(), condition.Type, string(condition.Status), r)
			}
		}
		return nil
	}
}

func hasStalledStatus(r metav1.ConditionStatus) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		rs := o.(*v1beta1.RepoSync)
		conditions := rs.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == v1beta1.RepoSyncStalled && condition.Status != r {
				return fmt.Errorf("object %q has %q condition status %q; want %q", o.GetName(), condition.Type, string(condition.Status), r)
			}
		}
		return nil
	}
}

func TestNamespaceRepo_Delegated(t *testing.T) {
	bsNamespaceRepo := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespaceRepo, configsync.RepoSyncName),
		ntopts.WithDelegatedControl,
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
	)

	nn := nomostest.RepoSyncNN(bsNamespaceRepo, configsync.RepoSyncName)
	repo, exist := nt.NonRootRepos[nn]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}

	// Validate service account 'store' not present.
	err := nt.ValidateNotFound("store", bsNamespaceRepo, &corev1.ServiceAccount{})
	if err != nil {
		nt.T.Errorf("store service account already present: %v", err)
	}

	sa := fake.ServiceAccountObject("store", core.Namespace(bsNamespaceRepo))
	repo.Add("acme/sa.yaml", sa)
	repo.CommitAndPush("Adding service account")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Validate service account 'store' is present.
	err = nt.Validate("store", bsNamespaceRepo, &corev1.ServiceAccount{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics from namespace reconciler.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(core.NsReconcilerName(bsNamespaceRepo, configsync.RepoSyncName),
			1, // 1 for the test ServiceAccount
			metrics.ResourceCreated("ServiceAccount"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestDeleteRepoSync_Delegated_AndRepoSyncV1Alpha1(t *testing.T) {
	bsNamespace := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithDelegatedControl,
	)

	var rs v1beta1.RepoSync
	if err := nt.Get(configsync.RepoSyncName, bsNamespace, &rs); err != nil {
		nt.T.Fatal(err)
	}
	secretNames := getNsReconcilerSecrets(nt, bsNamespace)

	// Delete RepoSync custom resource from the cluster.
	err := nt.Delete(&rs)
	if err != nil {
		nt.T.Fatalf("RepoSync delete failed: %v", err)
	}

	checkRepoSyncResourcesNotPresent(nt, bsNamespace, secretNames)

	nt.T.Log("Test RepoSync v1alpha1 version in delegated control mode")
	nn := nomostest.RepoSyncNN(bsNamespace, configsync.RepoSyncName)
	rsv1alpha1 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)
	if err := nt.Create(rsv1alpha1); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestDeleteRepoSync_Centralized_AndRepoSyncV1Alpha1(t *testing.T) {
	bsNamespace := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithCentralizedControl,
	)

	secretNames := getNsReconcilerSecrets(nt, bsNamespace)

	// Calculate the expected number of managed resource objects before removing the RepoSync.
	// This accounts for all the other objects in the RootSync repo from CentralizedControl.
	numObjects := nt.DefaultRootSyncObjectCount()
	numObjects-- // -1 because the RepoSync will be deleted manually

	// Remove RepoSync resource from Root Repository.
	nt.RootRepos[configsync.RootSyncName].Remove(nomostest.StructuredNSPath(bsNamespace, configsync.RepoSyncName))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing RepoSync from the Root Repository")

	// Remove from NamespaceRepos so we don't try to check that it is syncing,
	// as we've just deleted it.
	nn := nomostest.RepoSyncNN(bsNamespace, configsync.RepoSyncName)
	nsRepo := nt.NonRootRepos[nn]
	delete(nt.NonRootRepos, nn)
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	checkRepoSyncResourcesNotPresent(nt, bsNamespace, secretNames)

	// Validate multi-repo metrics from root reconciler.
	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			numObjects,
			metrics.ResourceDeleted(configsync.RepoSyncKind))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Test RepoSync v1alpha1 version in central control mode")
	nt.NonRootRepos[nn] = nsRepo
	rs := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(bsNamespace, rs.Name), rs)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add RepoSync v1alpha1")
	// Add the bookstore namespace repo back to NamespaceRepos to verify that it is synced.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestManageSelfRepoSync(t *testing.T) {
	bsNamespace := "bookstore"
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName))

	rs := &v1beta1.RepoSync{}
	if err := nt.Get(configsync.RepoSyncName, bsNamespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	sanitizedRs := fake.RepoSyncObjectV1Beta1(rs.Namespace, rs.Name)
	sanitizedRs.Spec = rs.Spec
	rsNN := nomostest.RepoSyncNN(rs.Namespace, rs.Name)
	nt.NonRootRepos[rsNN].Add("acme/repo-sync.yaml", sanitizedRs)
	nt.NonRootRepos[rsNN].CommitAndPush("add the repo-sync object that configures the reconciler")
	nt.WaitForRepoSyncSourceError(rs.Namespace, rs.Name, validate.SelfReconcileErrorCode, "RepoSync bookstore/repo-sync must not manage itself in its repo")
}

func getNsReconcilerSecrets(nt *nomostest.NT, ns string) []string {
	secretList := &corev1.SecretList{}
	if err := nt.List(secretList, client.InNamespace(configsync.ControllerNamespace)); err != nil {
		nt.T.Fatal(err)
	}
	var secretNames []string
	for _, secret := range secretList.Items {
		if strings.HasPrefix(secret.Name, core.NsReconcilerName(ns, configsync.RepoSyncName)) {
			secretNames = append(secretNames, secret.Name)
		}
	}
	return secretNames
}

func checkRepoSyncResourcesNotPresent(nt *nomostest.NT, namespace string, secretNames []string) {
	tg := taskgroup.New()
	tg.Go(func() error {
		return nomostest.WatchForNotFound(nt, kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, namespace)
	})
	tg.Go(func() error {
		return nomostest.WatchForNotFound(nt, kinds.Deployment(), core.NsReconcilerName(namespace, configsync.RepoSyncName), v1.NSConfigManagementSystem)
	})
	tg.Go(func() error {
		return nomostest.WatchForNotFound(nt, kinds.ConfigMap(), "ns-reconciler-bookstore-git-sync", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nomostest.WatchForNotFound(nt, kinds.ConfigMap(), "ns-reconciler-bookstore-reconciler", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nomostest.WatchForNotFound(nt, kinds.ConfigMap(), "ns-reconciler-bookstore-hydration-controller", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nomostest.WatchForNotFound(nt, kinds.ServiceAccount(), core.NsReconcilerName(namespace, configsync.RepoSyncName), configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nomostest.WatchForNotFound(nt, kinds.ServiceAccount(), controllers.RepoSyncPermissionsName(), configsync.ControllerNamespace)
	})
	for _, sName := range secretNames {
		nn := types.NamespacedName{Name: sName, Namespace: configsync.ControllerNamespace}
		tg.Go(func() error {
			return nomostest.WatchForNotFound(nt, kinds.Secret(), nn.Name, nn.Namespace)
		})
	}
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestDeleteNamespaceReconcilerDeployment(t *testing.T) {
	bsNamespace := "bookstore"
	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithCentralizedControl,
	)

	// Validate status condition "Reconciling" and Stalled is set to "False" after
	// the reconciler deployment is successfully created.
	// RepoSync status conditions "Reconciling" and "Stalled" are derived from
	// namespace reconciler deployment.
	// Retry before checking for Reconciling and Stalled conditions since the
	// reconcile request is received upon change in the reconciler deployment
	// conditions.
	// Here we are checking for false condition which requires atleast 2 reconcile
	// request to be processed by the controller.
	err := nomostest.WatchObject(nt, kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, bsNamespace,
		[]nomostest.Predicate{
			hasReconcilingStatus(metav1.ConditionFalse),
			hasStalledStatus(metav1.ConditionFalse),
		})
	if err != nil {
		nt.T.Errorf("RepoSync did not finish reconciling: %v", err)
	}

	// Delete namespace reconciler deployment in bookstore namespace.
	// The point here is to test that we properly respond to kubectl commands,
	// so this should NOT be replaced with nt.Delete.
	nsReconcilerDeployment := "ns-reconciler-bookstore"
	nt.MustKubectl("delete", "deployment", nsReconcilerDeployment,
		"-n", configsync.ControllerNamespace)

	// Verify that the deployment is re-created after deletion by checking the
	// Reconciling and Stalled condition in RepoSync resource.
	err = nomostest.WatchObject(nt, kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, bsNamespace,
		[]nomostest.Predicate{
			hasReconcilingStatus(metav1.ConditionFalse),
			hasStalledStatus(metav1.ConditionFalse),
		})
	if err != nil {
		nt.T.Errorf("RepoSync did not finish reconciling: %v", err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}
