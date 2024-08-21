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
	"time"

	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestDeleteRootSyncAndRootSyncV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	rs := &v1beta1.RootSync{}
	err := nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, rs)
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nomostest.DeleteObjectsAndWait(nt, rs); err != nil {
		nt.T.Fatal(err)
	}

	// Verify Root Reconciler deployment no longer present.
	_, err = retry.Retry(40*time.Second, func() error {
		var errs error
		errs = multierr.Append(errs, nt.ValidateNotFound(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, k8sobjects.DeploymentObject()))
		// validate Root Reconciler configmaps are no longer present.
		errs = multierr.Append(errs, nt.ValidateNotFound("root-reconciler-git-sync", configsync.ControllerNamespace, k8sobjects.ConfigMapObject()))
		errs = multierr.Append(errs, nt.ValidateNotFound("root-reconciler-reconciler", configsync.ControllerNamespace, k8sobjects.ConfigMapObject()))
		errs = multierr.Append(errs, nt.ValidateNotFound("root-reconciler-hydration-controller", configsync.ControllerNamespace, k8sobjects.ConfigMapObject()))
		errs = multierr.Append(errs, nt.ValidateNotFound("root-reconciler-source-format", configsync.ControllerNamespace, k8sobjects.ConfigMapObject()))
		// validate Root Reconciler ServiceAccount is no longer present.
		saName := core.RootReconcilerName(rs.Name)
		errs = multierr.Append(errs, nt.ValidateNotFound(saName, configsync.ControllerNamespace, k8sobjects.ServiceAccountObject(saName)))
		// validate Root Reconciler ClusterRoleBinding is no longer present.
		errs = multierr.Append(errs, nt.ValidateNotFound(controllers.RootSyncLegacyClusterRoleBindingName, configsync.ControllerNamespace, k8sobjects.ClusterRoleBindingObject()))
		return errs
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Test RootSync v1alpha1 version")
	rsv1alpha1 := nomostest.RootSyncObjectV1Alpha1FromRootRepo(nt, configsync.RootSyncName)
	if err := nt.KubeClient.Create(rsv1alpha1); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestUpdateRootSyncGitDirectory(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.SyncSource)
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)

	// Validate RootSync is present.
	var rs v1beta1.RootSync
	err := nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, &rs)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add audit namespace in policy directory acme.
	auditNS := "audit"
	auditNSObj := k8sobjects.NamespaceObject(auditNS)
	auditNSPath := fmt.Sprintf("%s/namespaces/%s/ns.yaml", rs.Spec.Git.Dir, auditNS)
	nt.Must(rootSyncGitRepo.Add(auditNSPath, auditNSObj))

	// Add namespace in policy directory 'foo'.
	fooDir := "foo"
	fooNS := "shipping"
	fooNSPath := fmt.Sprintf("%s/namespaces/%s/ns.yaml", fooDir, fooNS)
	fooNSObj := k8sobjects.NamespaceObject(fooNS)
	nt.Must(rootSyncGitRepo.Add(fooNSPath, fooNSObj))

	// Add repo resource in policy directory 'foo'.
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("%s/system/repo.yaml", fooDir),
		k8sobjects.RepoObject()))

	nt.Must(rootSyncGitRepo.CommitAndPush("add namespace to acme directory"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Validate namespace 'audit' created.
	err = nt.Validate(auditNS, "", auditNSObj)
	if err != nil {
		nt.T.Error(err)
	}

	// Validate namespace 'shipping' not present.
	err = nt.ValidateNotFound(fooNS, "", fooNSObj)
	if err != nil {
		nt.T.Errorf("%s present after deletion: %v", fooNS, err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, auditNSObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update RootSync.
	nomostest.SetPolicyDir(nt, configsync.RootSyncName, fooDir)
	syncDirectoryMap := map[types.NamespacedName]string{rootSyncNN: fooDir}
	err = nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirectoryMap))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate namespace 'shipping' created with the correct sourcePath annotation.
	if err := nt.Validate(fooNS, "", fooNSObj,
		testpredicates.HasAnnotation(metadata.SourcePathAnnotationKey, fooNSPath)); err != nil {
		nt.T.Error(err)
	}

	// Validate namespace 'audit' no longer present.
	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its finalizer will block until all objects in that namespace are
	// deleted.
	err = nt.Watcher.WatchForNotFound(kinds.Namespace(), auditNS, "")
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.Reset() // Not using the default PolicyDir
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, auditNSObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, fooNSObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestUpdateRootSyncGitBranch(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.SyncSource)
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)

	nsA := "ns-a"
	nsB := "ns-b"
	nsAObj := k8sobjects.NamespaceObject(nsA)
	nsBObj := k8sobjects.NamespaceObject(nsB)
	branchA := gitproviders.MainBranch
	branchB := "test-branch"

	// Add "ns-a" namespace.
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", nsA), nsAObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("add namespace to acme directory"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// "ns-a" namespace created
	err := nt.Validate(nsA, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsAObj)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add a 'test-branch' branch with 'ns-b' namespace.
	nt.Must(rootSyncGitRepo.CreateBranch(branchB))
	nt.Must(rootSyncGitRepo.CheckoutBranch(branchB))
	nt.Must(rootSyncGitRepo.Remove(fmt.Sprintf("acme/namespaces/%s/ns.yaml", nsA)))
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", nsB), nsBObj))
	nt.Must(rootSyncGitRepo.CommitAndPushBranch("add ns-b to acme directory", branchB))

	// Checkout back to 'main' branch to get the correct HEAD commit sha1.
	nt.Must(rootSyncGitRepo.CheckoutBranch(branchA))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// "ns-a" namespace still exists
	err = nt.Validate(nsA, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	// "ns-b" namespace not created
	err = nt.ValidateNotFound(nsB, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsAObj)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set branch to "test-branch"
	nomostest.SetGitBranch(nt, configsync.RootSyncName, branchB)

	// Checkout 'test-branch' branch to get the correct HEAD commit sha1.
	nt.Must(rootSyncGitRepo.CheckoutBranch(branchB))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// "ns-a" namespace deleted
	err = nt.ValidateNotFound(nsA, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	// "ns-b" namespace created
	err = nt.Validate(nsB, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, nsAObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsBObj)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set branch to "main"
	nomostest.SetGitBranch(nt, configsync.RootSyncName, branchA)

	// Checkout back to 'main' branch to get the correct HEAD commit sha1.
	nt.Must(rootSyncGitRepo.CheckoutBranch(branchA))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// "ns-a" namespace created
	err = nt.Validate(nsA, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	// "ns-b" namespace deleted
	err = nt.ValidateNotFound(nsB, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsAObj)
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, nsBObj)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestForceRevert(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)

	nt.Must(rootSyncGitRepo.Remove("acme/system/repo.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Cause source error"))

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, "")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := rootSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(rootSyncGitRepo.Git("reset", "--hard", "HEAD^"))
	nt.Must(rootSyncGitRepo.Push(syncBranch, "-f"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	if err := nomostest.ValidateStandardMetrics(nt); err != nil {
		nt.T.Fatal(err)
	}
}

func TestRootSyncReconcilingStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	// Validate status condition "Reconciling" is set to "False" after the Reconciler
	// Deployment is successfully created.
	// Log error if the Reconciling condition does not progress to False before the timeout
	// expires.
	err := nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		[]testpredicates.Predicate{
			hasRootSyncReconcilingStatus(metav1.ConditionFalse),
			hasRootSyncStalledStatus(metav1.ConditionFalse),
		},
		testwatcher.WatchTimeout(15*time.Second))
	if err != nil {
		nt.T.Errorf("RootSync did not finish reconciling: %v", err)
	}

	if err := nomostest.ValidateStandardMetrics(nt); err != nil {
		nt.T.Fatal(err)
	}
}

func TestManageSelfRootSync(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	rs := &v1beta1.RootSync{}
	if err := nt.KubeClient.Get(configsync.RootSyncName, configsync.ControllerNamespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	sanitizedRs := k8sobjects.RootSyncObjectV1Beta1(rs.Name)
	sanitizedRs.Spec = rs.Spec
	nt.Must(rootSyncGitRepo.Add("acme/root-sync.yaml", sanitizedRs))
	nt.Must(rootSyncGitRepo.CommitAndPush("add the root-sync object that configures the reconciler"))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, validate.SelfReconcileErrorCode, "RootSync config-management-system/root-sync must not manage itself in its repo")
}

func hasRootSyncReconcilingStatus(r metav1.ConditionStatus) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs := o.(*v1beta1.RootSync)
		conditions := rs.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == "Reconciling" && condition.Status != r {
				return fmt.Errorf("object %q have %q condition status %q; wanted %q", o.GetName(), condition.Type, string(condition.Status), r)
			}
		}
		return nil
	}
}

func hasRootSyncStalledStatus(r metav1.ConditionStatus) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs := o.(*v1beta1.RootSync)
		conditions := rs.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == "Stalled" && condition.Status != r {
				return fmt.Errorf("object %q have %q condition status %q; wanted %q", o.GetName(), condition.Type, string(condition.Status), r)
			}
		}
		return nil
	}
}
