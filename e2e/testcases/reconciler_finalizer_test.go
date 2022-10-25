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
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/common"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestReconcilerFinalizer_Orphan tests that the reconciler's finalizer
// correctly handles Orphan deletion propagation.
func TestReconcilerFinalizer_Orphan(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.MultiRepos)

	rootRepo := nt.RootRepos[rootSyncNN.Name]
	deployment1NN := types.NamespacedName{Name: "helloworld-1", Namespace: testNs}
	namespace1NN := types.NamespacedName{Name: testNs}

	nt.T.Cleanup(func() {
		cleanupSingleLevel(nt, rootSyncNN, deployment1NN, namespace1NN)
	})

	// Add namespace to RootSync
	namespace1 := fake.NamespaceObject(namespace1NN.Name)
	rootRepo.Add(nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name), namespace1)

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	rootRepo.Copy("../testdata/deployment-helloworld.yaml", deployment1Path)
	deployment1 := rootRepo.Get(deployment1Path)
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	nt.WaitForRepoSyncs()
	nomostest.WaitForCurrentStatus(nt, kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace())

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync are deleted, the
	// normal Cleanup won't be able to find the reconciler.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tailReconcilerLogs(ctx, nt, rootReconcilerObjectKey(rootSyncNN.Name))

	nt.T.Log("Disabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.Get(rootSync.GetName(), rootSync.GetNamespace(), rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if setDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyOrphan) {
		err = nt.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	nomostest.WatchForObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
		nomostest.StatusEquals(nt, kstatus.CurrentStatus),
		nomostest.MissingFinalizer(metadata.ReconcilerFinalizer),
	})

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err != nil {
		nt.T.Fatal(err)
	}

	nomostest.Wait(nt.T, fmt.Sprintf("%T %s finalization", rootSync, client.ObjectKeyFromObject(rootSync)), nt.DefaultWaitTimeout,
		func() error {
			var errs status.MultiError
			// The RootSync should skip finalizing and be deleted immediately
			errs = status.Append(errs, nt.ValidateNotFound(rootSync.GetName(), rootSync.GetNamespace(), &v1beta1.RootSync{}))
			// Namespace1 should NOT have been deleted, because it was orphaned by the RootSync.
			errs = status.Append(errs, nt.Validate(namespace1.GetName(), namespace1.GetNamespace(), &corev1.Namespace{},
				nomostest.HasAllNomosMetadata())) // metadata NOT removed when orphaned
			// Deployment1 should NOT have been deleted, because it was orphaned by the RootSync.
			errs = status.Append(errs, nt.Validate(deployment1.GetName(), deployment1.GetNamespace(), &appsv1.Deployment{},
				nomostest.HasAllNomosMetadata())) // metadata NOT removed when orphaned
			return errs
		},
	)
}

// TestReconcilerFinalizer_Foreground tests that the reconciler's finalizer
// correctly handles Foreground deletion propagation.
func TestReconcilerFinalizer_Foreground(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.MultiRepos)

	rootRepo := nt.RootRepos[rootSyncNN.Name]
	deployment1NN := types.NamespacedName{Name: "helloworld-1", Namespace: testNs}
	namespace1NN := types.NamespacedName{Name: testNs}

	nt.T.Cleanup(func() {
		cleanupSingleLevel(nt, rootSyncNN, deployment1NN, namespace1NN)
	})

	// Add namespace to RootSync
	namespace1 := fake.NamespaceObject(namespace1NN.Name)
	rootRepo.Add(nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name), namespace1)

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	rootRepo.Copy("../testdata/deployment-helloworld.yaml", deployment1Path)
	deployment1 := rootRepo.Get(deployment1Path)
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	nt.WaitForRepoSyncs()
	nomostest.WaitForCurrentStatus(nt, kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace())

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync are deleted, the
	// normal Cleanup won't be able to find the reconciler.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tailReconcilerLogs(ctx, nt, rootReconcilerObjectKey(rootSyncNN.Name))

	nt.T.Log("Enabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if setDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyForeground) {
		err = nt.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	nomostest.WatchForObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
		nomostest.StatusEquals(nt, kstatus.CurrentStatus),
		nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
	})

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err != nil {
		nt.T.Fatal(err)
	}

	nomostest.Wait(nt.T, fmt.Sprintf("%T %s finalization", rootSync, client.ObjectKeyFromObject(rootSync)), nt.DefaultWaitTimeout,
		func() error {
			var errs status.MultiError
			// Deleting the RootSync should trigger the RootSync's finalizer to delete the Deployment1 and Namespace
			errs = status.Append(errs, nt.ValidateNotFound(deployment1.GetName(), deployment1.GetNamespace(), &appsv1.Deployment{}))
			errs = status.Append(errs, nt.ValidateNotFound(namespace1.GetName(), namespace1.GetNamespace(), &corev1.Namespace{}))
			// After Deployment1 is deleted, the RootSync should have its finalizer removed and be garbage collected
			errs = status.Append(errs, nt.ValidateNotFound(rootSync.GetName(), rootSync.GetNamespace(), &v1beta1.RootSync{}))
			return errs
		},
	)
}

// TestReconcilerFinalizer_MultiLevelForeground tests that the reconciler's
// finalizer correctly handles multiple layers of Foreground deletion propagation.
func TestReconcilerFinalizer_MultiLevelForeground(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	repoSyncNN := nomostest.RepoSyncNN(testNs, "rs-test")
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name))

	rootRepo := nt.RootRepos[rootSyncNN.Name]
	nsRepo := nt.NonRootRepos[repoSyncNN]
	repoSyncPath := nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name)
	deployment1NN := types.NamespacedName{Name: "helloworld-1", Namespace: repoSyncNN.Namespace}
	deployment2NN := types.NamespacedName{Name: "helloworld-2", Namespace: repoSyncNN.Namespace}
	namespace1NN := types.NamespacedName{Name: repoSyncNN.Namespace}

	nt.T.Cleanup(func() {
		cleanupMultiLevel(nt,
			rootSyncNN,
			repoSyncNN,
			deployment1NN,
			deployment2NN,
			namespace1NN)
	})

	// Namespace created for the RepoSync by test setup
	namespace1 := rootRepo.Get(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Namespace))

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	rootRepo.Copy("../testdata/deployment-helloworld.yaml", deployment1Path)
	deployment1 := rootRepo.Get(deployment1Path)
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	nt.WaitForRepoSyncs()
	nomostest.WaitForCurrentStatus(nt, kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace())

	// Add deployment-helloworld-2 to RepoSync
	deployment2Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-2")
	nsRepo.Copy("../testdata/deployment-helloworld.yaml", deployment2Path)
	deployment2 := nsRepo.Get(deployment2Path)
	deployment2.SetName(deployment2NN.Name)
	deployment2.SetNamespace(deployment1NN.Namespace)
	nsRepo.Add(deployment2Path, deployment2)
	nsRepo.CommitAndPush("Adding deployment helloworld-2 to RepoSync")
	nt.WaitForRepoSyncs()
	nomostest.WaitForCurrentStatus(nt, kinds.Deployment(), deployment2.GetName(), deployment2.GetNamespace())

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync/RepoSync are deleted, the
	// normal Cleanup won't be able to find the reconcilers.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tailReconcilerLogs(ctx, nt, rootReconcilerObjectKey(rootSyncNN.Name))
	go tailReconcilerLogs(ctx, nt, nsReconcilerObjectKey(repoSyncNN.Namespace, repoSyncNN.Name))

	nt.T.Log("Enabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if setDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyForeground) {
		err = nt.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	nomostest.WatchForObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
		nomostest.StatusEquals(nt, kstatus.CurrentStatus),
		nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
	})

	nt.T.Log("Enabling RepoSync deletion propagation")
	repoSync := rootRepo.Get(repoSyncPath)
	if setDeletionPropagationPolicy(repoSync, metadata.DeletionPropagationPolicyForeground) {
		rootRepo.Add(repoSyncPath, repoSync)
		rootRepo.CommitAndPush("Enabling RepoSync deletion propagation")
		nt.WaitForRepoSyncs()
	}
	nomostest.WatchForObject(nt, kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace(), []nomostest.Predicate{
		nomostest.StatusEquals(nt, kstatus.CurrentStatus),
		nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
	})

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err != nil {
		nt.T.Fatal(err)
	}

	nomostest.Wait(nt.T, fmt.Sprintf("%T %s finalization", rootSync, client.ObjectKeyFromObject(rootSync)), nt.DefaultWaitTimeout,
		func() error {
			var errs status.MultiError
			// Deleting the RootSync should trigger the RootSync's finalizer to delete the RepoSync and Deployment1
			errs = status.Append(errs, nt.ValidateNotFound(deployment1.GetName(), deployment1.GetNamespace(), &appsv1.Deployment{}))
			// Deleting the RepoSync should trigger the RepoSync's finalizer to delete Deployment2
			errs = status.Append(errs, nt.ValidateNotFound(deployment2.GetName(), deployment2.GetNamespace(), &appsv1.Deployment{}))
			// After Deployment2 is deleted, the RepoSync should have its finalizer removed and be garbage collected
			errs = status.Append(errs, nt.ValidateNotFound(repoSync.GetName(), repoSync.GetNamespace(), &v1beta1.RepoSync{}))
			// After the RepoSync is gone, the garbage collector can finish finalizing the Namespace
			errs = status.Append(errs, nt.ValidateNotFound(namespace1.GetName(), namespace1.GetNamespace(), &corev1.Namespace{}))
			// After RepoSync is deleted, the RootSync should have its finalizer removed and be garbage collected
			errs = status.Append(errs, nt.ValidateNotFound(rootSync.GetName(), rootSync.GetNamespace(), &v1beta1.RootSync{}))
			return errs
		},
	)
}

// TestReconcilerFinalizer_MultiLevelMixed tests that the reconciler's finalizer
// correctly handles multiple layers of deletion propagation.
// The RootSync has Foreground policy, but manages a RepoSync with Orphan policy.
func TestReconcilerFinalizer_MultiLevelMixed(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	repoSyncNN := nomostest.RepoSyncNN(testNs, "rs-test")
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name))

	rootRepo := nt.RootRepos[rootSyncNN.Name]
	nsRepo := nt.NonRootRepos[repoSyncNN]
	repoSyncPath := nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name)
	deployment1NN := types.NamespacedName{Name: "helloworld-1", Namespace: repoSyncNN.Namespace}
	deployment2NN := types.NamespacedName{Name: "helloworld-2", Namespace: repoSyncNN.Namespace}
	namespace1NN := types.NamespacedName{Name: repoSyncNN.Namespace}

	nt.T.Cleanup(func() {
		cleanupMultiLevel(nt,
			rootSyncNN,
			repoSyncNN,
			deployment1NN,
			deployment2NN,
			namespace1NN)
	})

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment2NN.Namespace, "deployment-helloworld-1")
	rootRepo.Copy("../testdata/deployment-helloworld.yaml", deployment1Path)
	deployment1 := rootRepo.Get(deployment1Path)
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment2NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	nt.WaitForRepoSyncs()
	nomostest.WaitForCurrentStatus(nt, kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace())

	// Add deployment-helloworld-2 to RepoSync
	deployment2Path := nomostest.StructuredNSPath(deployment2NN.Namespace, "deployment-helloworld-2")
	nsRepo.Copy("../testdata/deployment-helloworld.yaml", deployment2Path)
	deployment2 := nsRepo.Get(deployment2Path)
	deployment2.SetName(deployment2NN.Name)
	deployment2.SetNamespace(deployment2NN.Namespace)
	nsRepo.Add(deployment2Path, deployment2)
	nsRepo.CommitAndPush("Adding deployment helloworld-2 to RepoSync")
	nt.WaitForRepoSyncs()
	nomostest.WaitForCurrentStatus(nt, kinds.Deployment(), deployment2.GetName(), deployment2.GetNamespace())

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync/RepoSync are deleted, the
	// normal Cleanup won't be able to find the reconcilers.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go tailReconcilerLogs(ctx, nt, rootReconcilerObjectKey(rootSyncNN.Name))
	go tailReconcilerLogs(ctx, nt, nsReconcilerObjectKey(repoSyncNN.Namespace, repoSyncNN.Name))

	nt.T.Log("Enabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if setDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyForeground) {
		err = nt.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	nomostest.WatchForObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
		nomostest.StatusEquals(nt, kstatus.CurrentStatus),
		nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
	})

	nt.T.Log("Disabling RepoSync deletion propagation")
	repoSync := rootRepo.Get(repoSyncPath)
	if removeDeletionPropagationPolicy(repoSync) {
		rootRepo.Add(repoSyncPath, repoSync)
		rootRepo.CommitAndPush("Disabling RepoSync deletion propagation")
		nt.WaitForRepoSyncs()
	}
	nomostest.WatchForObject(nt, kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace(), []nomostest.Predicate{
		nomostest.StatusEquals(nt, kstatus.CurrentStatus),
		nomostest.MissingFinalizer(metadata.ReconcilerFinalizer),
	})

	// Abandon the test namespace, otherwise it will block the finalizer
	namespace1Path := nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name)
	namespace1 := rootRepo.Get(namespace1Path)
	core.SetAnnotation(namespace1, common.LifecycleDeleteAnnotation, common.PreventDeletion)
	rootRepo.Add(namespace1Path, namespace1)
	rootRepo.CommitAndPush("Adding annotation to keep test namespace on removal from git")
	nt.WaitForRepoSyncs()

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err != nil {
		nt.T.Fatal(err)
	}

	nomostest.Wait(nt.T, fmt.Sprintf("%T %s finalization", rootSync, client.ObjectKeyFromObject(rootSync)), nt.DefaultWaitTimeout,
		func() error {
			var errs status.MultiError
			// Deleting the RootSync should trigger the RootSync's finalizer to delete the RepoSync and Deployment1
			errs = status.Append(errs, nt.ValidateNotFound(deployment1.GetName(), deployment1.GetNamespace(), &appsv1.Deployment{}))
			// RepoSync should delete quickly without finalizing
			errs = status.Append(errs, nt.ValidateNotFound(repoSync.GetName(), repoSync.GetNamespace(), &v1beta1.RepoSync{}))
			// After RepoSync and Deployment1 are deleted, the RootSync should have its finalizer removed and be garbage collected
			errs = status.Append(errs, nt.ValidateNotFound(rootSync.GetName(), rootSync.GetNamespace(), &v1beta1.RootSync{}))
			// Namespace1 should NOT have been deleted, because it was abandoned by the RootSync.
			// TODO: Use NoConfigSyncMetadata predicate once metadata removal is fixed (b/256043590)
			errs = status.Append(errs, nt.Validate(namespace1.GetName(), namespace1.GetNamespace(), &corev1.Namespace{}))
			// Deployment2 should NOT have been deleted, because it was orphaned by the RepoSync.
			errs = status.Append(errs, nt.Validate(deployment2.GetName(), deployment2.GetNamespace(), &appsv1.Deployment{},
				nomostest.HasAllNomosMetadata())) // metadata NOT removed when orphaned
			return errs
		},
	)
}

// tailReconcilerLogs starts tailing a reconciler's logs.
// The logs are stored in memory until either the context is cancelled or the
// kubectl command exits (usually because the container exited).
// This allows capturing logs even if the reconciler is deleted before the
// test ends.
// The logs will only be printed if the test has failed when the command exits.
// Run in an goroutine to capture logs in the background while deleting RSyncs.
func tailReconcilerLogs(ctx context.Context, nt *nomostest.NT, reconcilerNN types.NamespacedName) {
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
		nt.T.Logf("Reconciler logs:\n%s", string(out))
	}
}

func cleanupSingleLevel(nt *nomostest.NT, rootSyncNN, deployment1NN, namespace1NN types.NamespacedName) {
	nt.T.Log("Stopping webhook")
	// Stop webhook to avoid deletion prevention.
	// Webhook will be re-enabled by test setup, if the next test needs it.
	nomostest.StopWebhook(nt)

	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	deployment1 := fake.DeploymentObject(core.Name(deployment1NN.Name), core.Namespace(deployment1NN.Namespace))
	namespace1 := fake.NamespaceObject(namespace1NN.Name)

	nt.T.Log("Resetting RootSync deletion propagation")
	err := nt.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// ValidateMultiRepoDeployments called by WaitForRepoSyncs handles RootSync re-creation
			return
		}
		nt.T.Fatal(err)
	}
	if removeDeletionPropagationPolicy(rootSync) {
		err = nt.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	nt.T.Logf("Deleting RootSync %s", rootSyncNN)
	err = nt.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	// Delete Deployment 1
	nt.T.Logf("Deleting Deployment %s", deployment1NN)
	err = nt.Delete(deployment1, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	// Delete Namespace 1
	nt.T.Logf("Deleting Namespace %s", namespace1NN)
	err = nt.Delete(namespace1, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	nomostest.Wait(nt.T, "test cleanup", nt.DefaultWaitTimeout,
		func() error {
			var errs status.MultiError
			errs = status.Append(errs, nt.ValidateNotFound(rootSync.GetName(), rootSync.GetNamespace(), &v1beta1.RootSync{}))
			errs = status.Append(errs, nt.ValidateNotFound(deployment1.GetName(), deployment1.GetNamespace(), &appsv1.Deployment{}))
			errs = status.Append(errs, nt.ValidateNotFound(namespace1.GetName(), namespace1.GetNamespace(), &corev1.Namespace{}))
			return errs
		},
	)
}

func cleanupMultiLevel(nt *nomostest.NT, rootSyncNN, repoSyncNN, deployment1NN, deployment2NN, namespace1NN types.NamespacedName) {
	nt.T.Log("Stopping webhook")
	// Stop webhook to avoid deletion prevention.
	// Webhook will be re-enabled by test setup, if the next test needs it.
	nomostest.StopWebhook(nt)

	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	repoSync := fake.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name)
	deployment1 := fake.DeploymentObject(core.Name(deployment1NN.Name), core.Namespace(deployment1NN.Namespace))
	deployment2 := fake.DeploymentObject(core.Name(deployment2NN.Name), core.Namespace(deployment2NN.Namespace))
	namespace1 := fake.NamespaceObject(namespace1NN.Name)

	nt.T.Log("Resetting RootSync deletion propagation")
	err := nt.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// ValidateMultiRepoDeployments called by WaitForRepoSyncs handles RootSync re-creation
			return
		}
		nt.T.Fatal(err)
	}
	if removeDeletionPropagationPolicy(rootSync) {
		err = nt.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	nt.T.Logf("Deleting RootSync %s", rootSyncNN)
	err = nt.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	nt.T.Log("Resetting RepoSync deletion propagation")
	err = nt.Get(repoSync.Name, repoSync.Namespace, repoSync)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	} else if removeDeletionPropagationPolicy(repoSync) {
		err = nt.Update(repoSync)
		if err != nil {
			nt.T.Error(err)
		}
	}

	nt.T.Logf("Deleting RepoSync %s", repoSyncNN)
	err = nt.Delete(repoSync, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	// Delete Deployment 1
	nt.T.Logf("Deleting Deployment %s", deployment1NN)
	err = nt.Delete(deployment1, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	// Delete Deployment 2
	nt.T.Logf("Deleting Deployment %s", deployment2NN)
	err = nt.Delete(deployment2, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	// Delete Namespace 1
	nt.T.Logf("Deleting Namespace %s", namespace1NN)
	err = nt.Delete(namespace1, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	}

	nomostest.Wait(nt.T, "test cleanup", nt.DefaultWaitTimeout,
		func() error {
			var errs status.MultiError
			errs = status.Append(errs, nt.ValidateNotFound(rootSync.GetName(), rootSync.GetNamespace(), &v1beta1.RootSync{}))
			errs = status.Append(errs, nt.ValidateNotFound(repoSync.GetName(), repoSync.GetNamespace(), &v1beta1.RepoSync{}))
			errs = status.Append(errs, nt.ValidateNotFound(deployment1.GetName(), deployment1.GetNamespace(), &appsv1.Deployment{}))
			errs = status.Append(errs, nt.ValidateNotFound(deployment2.GetName(), deployment2.GetNamespace(), &appsv1.Deployment{}))
			errs = status.Append(errs, nt.ValidateNotFound(namespace1.GetName(), namespace1.GetNamespace(), &corev1.Namespace{}))
			return errs
		},
	)
}

// rootReconcilerObjectKey returns an ObjectKey for interacting with the
// RootReconciler for the specified RootSync.
func rootReconcilerObjectKey(syncName string) client.ObjectKey {
	return client.ObjectKey{
		Name:      core.RootReconcilerName(syncName),
		Namespace: configsync.ControllerNamespace,
	}
}

// nsReconcilerObjectKey returns an ObjectKey for interacting with the
// NsReconciler for the specified RepoSync.
func nsReconcilerObjectKey(namespace, syncName string) client.ObjectKey {
	return client.ObjectKey{
		Name:      core.NsReconcilerName(namespace, syncName),
		Namespace: configsync.ControllerNamespace,
	}
}

func setDeletionPropagationPolicy(obj client.Object, policy metadata.DeletionPropagationPolicy) bool {
	annotations := obj.GetAnnotations()
	if len(annotations) == 0 {
		annotations = map[string]string{}
	} else if val, found := annotations[metadata.DeletionPropagationPolicyAnnotationKey]; found && val == string(policy) {
		return false
	}
	annotations[metadata.DeletionPropagationPolicyAnnotationKey] = string(policy)
	obj.SetAnnotations(annotations)
	return true
}

func removeDeletionPropagationPolicy(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if len(annotations) == 0 {
		return false
	}
	if _, found := annotations[metadata.DeletionPropagationPolicyAnnotationKey]; !found {
		return false
	}
	delete(annotations, metadata.DeletionPropagationPolicyAnnotationKey)
	obj.SetAnnotations(annotations)
	return true
}
