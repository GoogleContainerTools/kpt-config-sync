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
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNamespaceRepo_Centralized(t *testing.T) {
	bsNamespace := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithCentralizedControl,
	)

	// Validate status condition "Reconciling" and "Stalled "is set to "False"
	// after the reconciler deployment is successfully created.
	// RepoSync status conditions "Reconciling" and "Stalled" are derived from
	// namespace reconciler deployment.
	// Log error if the Reconciling condition does not progress to False before
	// the timeout expires.
	_, err := nomostest.Retry(15*time.Second, func() error {
		return nt.Validate("repo-sync", bsNamespace, &v1beta1.RepoSync{},
			hasReconcilingStatus(metav1.ConditionFalse), hasStalledStatus(metav1.ConditionFalse))
	})
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
	nt.WaitForRepoSyncs()

	// Validate service account 'store' is present.
	_, err = nomostest.Retry(15*time.Second, func() error {
		return nt.Validate("store", bsNamespace, &corev1.ServiceAccount{})
	})
	if err != nil {
		nt.T.Fatalf("service account store not found: %v", err)
	}

	// Validate multi-repo metrics from namespace reconciler.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(core.NsReconcilerName(bsNamespace, configsync.RepoSyncName), 1, metrics.ResourceCreated("ServiceAccount"))
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
		ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(bsNamespaceRepo, configsync.RepoSyncName),
		ntopts.WithDelegatedControl,
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
	nt.WaitForRepoSyncs()

	// Validate service account 'store' is present.
	err = nt.Validate("store", bsNamespaceRepo, &corev1.ServiceAccount{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics from namespace reconciler.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(core.NsReconcilerName(bsNamespaceRepo, configsync.RepoSyncName), 1, metrics.ResourceCreated("ServiceAccount"))
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
		ntopts.SkipMonoRepo,
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

	checkRepoSyncResourcesNotPresent(bsNamespace, secretNames, nt)

	nt.T.Log("Test RepoSync v1alpha1 version in delegated control mode")
	nn := nomostest.RepoSyncNN(bsNamespace, configsync.RepoSyncName)
	rsv1alpha1 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)
	if err := nt.Create(rsv1alpha1); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncs()
}

func TestDeleteRepoSync_Centralized_AndRepoSyncV1Alpha1(t *testing.T) {
	bsNamespace := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithCentralizedControl,
	)

	secretNames := getNsReconcilerSecrets(nt, bsNamespace)

	// Remove RepoSync resource from Root Repository.
	nt.RootRepos[configsync.RootSyncName].Remove(nomostest.StructuredNSPath(bsNamespace, configsync.RepoSyncName))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing RepoSync from the Root Repository")
	// Remove from NamespaceRepos so we don't try to check that it is syncing,
	// as we've just deleted it.
	nn := nomostest.RepoSyncNN(bsNamespace, configsync.RepoSyncName)
	nsRepo := nt.NonRootRepos[nn]
	delete(nt.NonRootRepos, nn)
	nt.WaitForRepoSyncs()

	checkRepoSyncResourcesNotPresent(bsNamespace, secretNames, nt)

	// Validate multi-repo metrics from root reconciler.
	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		var err error
		// TODO: Remove the psp related change when Kubernetes 1.25 is
		// available on GKE.
		if strings.Contains(os.Getenv("GCP_CLUSTER"), "psp") {
			err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 5,
				metrics.ResourceDeleted(configsync.RepoSyncKind))
		} else {
			err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 4,
				metrics.ResourceDeleted(configsync.RepoSyncKind))
		}
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
	nt.WaitForRepoSyncs()
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

func checkRepoSyncResourcesNotPresent(namespace string, secretNames []string, nt *nomostest.NT) {
	_, err := nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound(configsync.RepoSyncName, namespace, fake.RepoSyncObjectV1Beta1(namespace, configsync.RepoSyncName))
	})
	if err != nil {
		nt.T.Errorf("RepoSync present after deletion: %v", err)
	}

	// Verify Namespace Reconciler deployment no longer present.
	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound(core.NsReconcilerName(namespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, fake.DeploymentObject())
	})
	if err != nil {
		nt.T.Errorf("Reconciler deployment present after deletion: %v", err)
	}

	// Verify Namespace Reconciler configmaps no longer present.
	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound("ns-reconciler-bookstore-git-sync", configsync.ControllerNamespace, fake.ConfigMapObject())
	})
	if err != nil {
		nt.T.Errorf("Configmap ns-reconciler-bookstore-git-sync present after deletion: %v", err)
	}

	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound("ns-reconciler-bookstore-reconciler", configsync.ControllerNamespace, fake.ConfigMapObject())
	})
	if err != nil {
		nt.T.Errorf("Configmap reconciler-bookstore-reconciler present after deletion: %v", err)
	}

	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound("ns-reconciler-bookstore-hydration-controller", configsync.ControllerNamespace, fake.ConfigMapObject())
	})
	if err != nil {
		nt.T.Errorf("Configmap ns-reconciler-bookstore-hydration-controller present after deletion: %v", err)
	}

	// Verify Namespace Reconciler service account no longer present.
	saName := core.NsReconcilerName(namespace, configsync.RepoSyncName)
	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound(saName, configsync.ControllerNamespace, fake.ServiceAccountObject(saName))
	})
	if err != nil {
		nt.T.Errorf("ServiceAccount %s present after deletion: %v", saName, err)
	}

	// Verify Namespace Reconciler role binding no longer present.
	rbName := controllers.RepoSyncPermissionsName()
	_, err = nomostest.Retry(5*time.Second, func() error {
		return nt.ValidateNotFound(rbName, configsync.ControllerNamespace, fake.RoleBindingObject())
	})
	if err != nil {
		nt.T.Errorf("RoleBinding %s present after deletion: %v", rbName, err)
	}

	// Verify Namespace Reconciler secrets no longer present.
	_, err = nomostest.Retry(5*time.Second, func() error {
		for _, sName := range secretNames {
			if err := nt.ValidateNotFound(sName, configsync.ControllerNamespace, fake.SecretObject(sName)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		nt.T.Errorf("ServiceAccount %s present after deletion: %v", saName, err)
	}
}

func TestDeleteNamespaceReconcilerDeployment(t *testing.T) {
	bsNamespace := "bookstore"
	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.SkipMonoRepo,
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
	_, err := nomostest.Retry(nt.DefaultWaitTimeout, func() error {
		return nt.Validate(configsync.RepoSyncName, bsNamespace, &v1beta1.RepoSync{},
			hasReconcilingStatus(metav1.ConditionFalse), hasStalledStatus(metav1.ConditionFalse))
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
	_, err = nomostest.Retry(nt.DefaultWaitTimeout, func() error {
		return nt.Validate(configsync.RepoSyncName, bsNamespace, &v1beta1.RepoSync{},
			hasReconcilingStatus(metav1.ConditionFalse), hasStalledStatus(metav1.ConditionFalse))
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
