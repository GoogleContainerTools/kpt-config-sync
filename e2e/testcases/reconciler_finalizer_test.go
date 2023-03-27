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
	"os"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/common"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// TestReconcilerFinalizer_Orphan tests that the reconciler's finalizer
// correctly handles Orphan deletion propagation.
func TestReconcilerFinalizer_Orphan(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.MultiRepos)

	rootRepo := nt.RootRepos[rootSyncNN.Name]
	deployment1NN := types.NamespacedName{Name: "helloworld-1", Namespace: testNs}
	namespace1NN := types.NamespacedName{Name: testNs}
	safetyNamespace1NN := types.NamespacedName{Name: rootRepo.SafetyNSName}

	nt.T.Cleanup(func() {
		cleanupSingleLevel(nt,
			rootSyncNN,
			deployment1NN,
			namespace1NN, safetyNamespace1NN)
	})

	// Add namespace to RootSync
	namespace1 := fake.NamespaceObject(namespace1NN.Name)
	rootRepo.Add(nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name), namespace1)

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	deployment1 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nomostest.WatchForCurrentStatus(nt, kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync are deleted, the
	// normal Cleanup won't be able to find the reconciler.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go nomostest.TailReconcilerLogs(ctx, nt, nomostest.RootReconcilerObjectKey(rootSyncNN.Name))

	nt.T.Log("Disabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.KubeClient.Get(rootSync.GetName(), rootSync.GetNamespace(), rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if nomostest.SetDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyOrphan) {
		err = nt.KubeClient.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	require.NoError(nt.T,
		nomostest.WatchObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
			nomostest.StatusEquals(nt, kstatus.CurrentStatus),
			nomostest.MissingFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.KubeClient.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
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
	safetyNamespace1NN := types.NamespacedName{Name: rootRepo.SafetyNSName}

	nt.T.Cleanup(func() {
		cleanupSingleLevel(nt,
			rootSyncNN,
			deployment1NN,
			namespace1NN, safetyNamespace1NN)
	})

	// Add namespace to RootSync
	namespace1 := fake.NamespaceObject(namespace1NN.Name)
	rootRepo.Add(nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name), namespace1)

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	deployment1 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nomostest.WatchForCurrentStatus(nt, kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync are deleted, the
	// normal Cleanup won't be able to find the reconciler.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go nomostest.TailReconcilerLogs(ctx, nt, nomostest.RootReconcilerObjectKey(rootSyncNN.Name))

	nt.T.Log("Enabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.KubeClient.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if nomostest.SetDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyForeground) {
		err = nt.KubeClient.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	require.NoError(nt.T,
		nomostest.WatchObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
			nomostest.StatusEquals(nt, kstatus.CurrentStatus),
			nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.KubeClient.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
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
	nt := nomostest.New(t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name),
		ntopts.RepoSyncPermissions(policy.AppsAdmin(), policy.CoreAdmin()), // NS Reconciler manages Deployments
	)

	rootRepo := nt.RootRepos[rootSyncNN.Name]
	nsRepo := nt.NonRootRepos[repoSyncNN]
	repoSyncPath := nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name)
	deployment1NN := types.NamespacedName{Name: "helloworld-1", Namespace: repoSyncNN.Namespace}
	deployment2NN := types.NamespacedName{Name: "helloworld-2", Namespace: repoSyncNN.Namespace}
	namespace1NN := types.NamespacedName{Name: repoSyncNN.Namespace}
	safetyNamespace1NN := types.NamespacedName{Name: rootRepo.SafetyNSName}
	safetyNamespace2NN := types.NamespacedName{Name: nsRepo.SafetyNSName}

	nt.T.Cleanup(func() {
		cleanupMultiLevel(nt,
			rootSyncNN, repoSyncNN,
			deployment1NN, deployment2NN,
			namespace1NN, safetyNamespace1NN, safetyNamespace2NN)
	})

	// Namespace created for the RepoSync by test setup
	namespace1 := rootRepo.Get(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Namespace))

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	deployment1 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nomostest.WatchForCurrentStatus(nt, kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Add deployment-helloworld-2 to RepoSync
	deployment2Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-2")
	deployment2 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment2.SetName(deployment2NN.Name)
	deployment2.SetNamespace(deployment1NN.Namespace)
	nsRepo.Add(deployment2Path, deployment2)
	nsRepo.CommitAndPush("Adding deployment helloworld-2 to RepoSync")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nomostest.WatchForCurrentStatus(nt, kinds.Deployment(), deployment2.Name, deployment2.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync/RepoSync are deleted, the
	// normal Cleanup won't be able to find the reconcilers.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go nomostest.TailReconcilerLogs(ctx, nt, nomostest.RootReconcilerObjectKey(rootSyncNN.Name))
	go nomostest.TailReconcilerLogs(ctx, nt, nomostest.NsReconcilerObjectKey(repoSyncNN.Namespace, repoSyncNN.Name))

	nt.T.Log("Enabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.KubeClient.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if nomostest.SetDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyForeground) {
		err = nt.KubeClient.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	require.NoError(nt.T,
		nomostest.WatchObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
			nomostest.StatusEquals(nt, kstatus.CurrentStatus),
			nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	nt.T.Log("Enabling RepoSync deletion propagation")
	repoSync := rootRepo.Get(repoSyncPath)
	if nomostest.SetDeletionPropagationPolicy(repoSync, metadata.DeletionPropagationPolicyForeground) {
		rootRepo.Add(repoSyncPath, repoSync)
		rootRepo.CommitAndPush("Enabling RepoSync deletion propagation")
	}
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	require.NoError(nt.T,
		nomostest.WatchObject(nt, kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace(), []nomostest.Predicate{
			nomostest.StatusEquals(nt, kstatus.CurrentStatus),
			nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.KubeClient.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
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
	nt := nomostest.New(t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name),
		ntopts.RepoSyncPermissions(policy.AppsAdmin(), policy.CoreAdmin()), // NS Reconciler manages Deployments
	)

	rootRepo := nt.RootRepos[rootSyncNN.Name]
	nsRepo := nt.NonRootRepos[repoSyncNN]
	repoSyncPath := nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name)
	deployment1NN := types.NamespacedName{Name: "helloworld-1", Namespace: repoSyncNN.Namespace}
	deployment2NN := types.NamespacedName{Name: "helloworld-2", Namespace: repoSyncNN.Namespace}
	namespace1NN := types.NamespacedName{Name: repoSyncNN.Namespace}
	safetyNamespace1NN := types.NamespacedName{Name: rootRepo.SafetyNSName}
	safetyNamespace2NN := types.NamespacedName{Name: nsRepo.SafetyNSName}

	nt.T.Cleanup(func() {
		cleanupMultiLevel(nt,
			rootSyncNN, repoSyncNN,
			deployment1NN, deployment2NN,
			namespace1NN, safetyNamespace1NN, safetyNamespace2NN)
	})

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment2NN.Namespace, "deployment-helloworld-1")
	deployment1 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment2NN.Namespace)
	rootRepo.Add(deployment1Path, deployment1)
	rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nomostest.WatchForCurrentStatus(nt, kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Add deployment-helloworld-2 to RepoSync
	deployment2Path := nomostest.StructuredNSPath(deployment2NN.Namespace, "deployment-helloworld-2")
	deployment2 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment2.SetName(deployment2NN.Name)
	deployment2.SetNamespace(deployment2NN.Namespace)
	nsRepo.Add(deployment2Path, deployment2)
	nsRepo.CommitAndPush("Adding deployment helloworld-2 to RepoSync")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nomostest.WatchForCurrentStatus(nt, kinds.Deployment(), deployment2.Name, deployment2.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Tail reconciler logs and print if there's an error.
	// This is necessary because if the RootSync/RepoSync are deleted, the
	// normal Cleanup won't be able to find the reconcilers.
	// Start here to catch both the finalizer injection and deletion behavior.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go nomostest.TailReconcilerLogs(ctx, nt, nomostest.RootReconcilerObjectKey(rootSyncNN.Name))
	go nomostest.TailReconcilerLogs(ctx, nt, nomostest.NsReconcilerObjectKey(repoSyncNN.Namespace, repoSyncNN.Name))

	nt.T.Log("Enabling RootSync deletion propagation")
	rootSync := fake.RootSyncObjectV1Beta1(rootSyncNN.Name)
	err := nt.KubeClient.Get(rootSync.Name, rootSync.Namespace, rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}
	if nomostest.SetDeletionPropagationPolicy(rootSync, metadata.DeletionPropagationPolicyForeground) {
		err = nt.KubeClient.Update(rootSync)
		if err != nil {
			nt.T.Fatal(err)
		}
	}
	require.NoError(nt.T,
		nomostest.WatchObject(nt, kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []nomostest.Predicate{
			nomostest.StatusEquals(nt, kstatus.CurrentStatus),
			nomostest.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	nt.T.Log("Disabling RepoSync deletion propagation")
	repoSync := rootRepo.Get(repoSyncPath)
	if nomostest.RemoveDeletionPropagationPolicy(repoSync) {
		rootRepo.Add(repoSyncPath, repoSync)
		rootRepo.CommitAndPush("Disabling RepoSync deletion propagation")
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}
	}
	require.NoError(nt.T,
		nomostest.WatchObject(nt, kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace(), []nomostest.Predicate{
			nomostest.StatusEquals(nt, kstatus.CurrentStatus),
			nomostest.MissingFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Abandon the test namespace, otherwise it will block the finalizer
	namespace1Path := nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name)
	namespace1 := rootRepo.Get(namespace1Path)
	core.SetAnnotation(namespace1, common.LifecycleDeleteAnnotation, common.PreventDeletion)
	rootRepo.Add(namespace1Path, namespace1)
	rootRepo.CommitAndPush("Adding annotation to keep test namespace on removal from git")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Delete the RootSync
	// DeletePropagationBackground is required when deleting RootSync, to
	// avoid causing the reconciler Deployment to be deleted before the RootSync
	// finishes finalizing.
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.KubeClient.Delete(rootSync, client.PropagationPolicy(metav1.DeletePropagationBackground))
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

func cleanupSingleLevel(nt *nomostest.NT,
	rootSyncNN,
	deployment1NN,
	namespace1NN, safetyNamespace1NN types.NamespacedName,
) {
	cleanupSyncsAndObjects(nt,
		[]client.Object{
			fake.RootSyncObjectV1Beta1(rootSyncNN.Name),
		},
		[]client.Object{
			fake.DeploymentObject(core.Name(deployment1NN.Name), core.Namespace(deployment1NN.Namespace)),
			fake.NamespaceObject(namespace1NN.Name),
			fake.NamespaceObject(safetyNamespace1NN.Name),
		})
}

func cleanupMultiLevel(nt *nomostest.NT,
	rootSyncNN, repoSyncNN,
	deployment1NN, deployment2NN,
	namespace1NN, safetyNamespace1NN, safetyNamespace2NN types.NamespacedName,
) {
	cleanupSyncsAndObjects(nt,
		[]client.Object{
			fake.RootSyncObjectV1Beta1(rootSyncNN.Name),
			fake.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name),
		},
		[]client.Object{
			fake.DeploymentObject(core.Name(deployment1NN.Name), core.Namespace(deployment1NN.Namespace)),
			fake.DeploymentObject(core.Name(deployment2NN.Name), core.Namespace(deployment2NN.Namespace)),
			fake.NamespaceObject(namespace1NN.Name),
			fake.NamespaceObject(safetyNamespace1NN.Name),
			fake.NamespaceObject(safetyNamespace2NN.Name),
		})
}

func cleanupSyncsAndObjects(nt *nomostest.NT, syncObjs []client.Object, objs []client.Object) {
	if nt.T.Failed() && *e2e.Debug {
		nt.T.Log("Skipping test cleanup: debug enabled")
		return
	}

	nt.T.Log("Stopping webhook")
	// Stop webhook to avoid deletion prevention.
	// Webhook will be re-enabled by test setup, if the next test needs it.
	nomostest.StopWebhook(nt)

	// For the purposes of these finalizer tests, we assume the finalizer may
	// not work correctly. So we delete the deletion propagation annotation,
	// the syncs, and all the managed objects.
	for _, syncObj := range syncObjs {
		if err := deleteSyncWithOrphanPolicy(nt, syncObj); err != nil {
			nt.T.Error(err)
		}
	}
	for _, obj := range objs {
		if err := deleteObject(nt, obj); err != nil {
			nt.T.Error(err)
		}
	}

	nomostest.Wait(nt.T, "test cleanup", nt.DefaultWaitTimeout,
		func() error {
			var errs status.MultiError
			for _, syncObj := range syncObjs {
				errs = status.Append(errs, validateNotFoundFromObject(nt, syncObj))
			}
			for _, obj := range objs {
				errs = status.Append(errs, validateNotFoundFromObject(nt, obj))
			}
			return errs
		},
	)
}

// validateNotFoundFromObject wraps nt.ValidateNotFound, but allows the object
// to be fully populated, constructing a new empty typed object as needed.
func validateNotFoundFromObject(nt *nomostest.NT, obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	obj, _, err := newEmptyTypedObject(nt, obj)
	if err != nil {
		return err
	}
	return nt.ValidateNotFound(key.Name, key.Namespace, obj)
}

func newEmptyTypedObject(nt *nomostest.NT, obj client.Object) (client.Object, schema.GroupVersionKind, error) {
	scheme := nt.KubeClient.Client.Scheme()
	gvk, err := kinds.Lookup(obj, scheme)
	if err != nil {
		return nil, gvk, err
	}
	rObj, err := scheme.New(gvk)
	if err != nil {
		return nil, gvk, err
	}
	cObj, ok := rObj.(client.Object)
	if !ok {
		return nil, gvk, errors.Errorf("failed to cast %s %T to client.Object", gvk.Kind, rObj)
	}
	return cObj, gvk, nil
}

func deleteSyncWithOrphanPolicy(nt *nomostest.NT, obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	obj, gvk, err := newEmptyTypedObject(nt, obj)
	if err != nil {
		return err
	}

	err = nt.KubeClient.Get(key.Name, key.Name, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	nt.T.Log("Removing deletion propagation annotation")
	if nomostest.RemoveDeletionPropagationPolicy(obj) {
		err = nt.KubeClient.Update(obj)
		if err != nil {
			return err
		}
	}

	nt.T.Logf("Deleting %s %s", gvk.Kind, key)
	// TODO: Remove explicit Background policy after the reconciler-manager finalizer is added.
	err = nt.KubeClient.Delete(obj, client.PropagationPolicy(metav1.DeletePropagationBackground))
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func deleteObject(nt *nomostest.NT, obj client.Object) error {
	gvk, err := kinds.Lookup(obj, nt.KubeClient.Client.Scheme())
	if err != nil {
		return err
	}

	nt.T.Logf("Deleting %s %s", gvk.Kind, client.ObjectKeyFromObject(obj))
	err = nt.KubeClient.Delete(obj, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func loadDeployment(nt *nomostest.NT, path string) *appsv1.Deployment {
	specBytes, err := os.ReadFile(path)
	if err != nil {
		nt.T.Fatalf("failed to read test file: %v", err)
	}
	obj := &appsv1.Deployment{}
	err = yaml.Unmarshal(specBytes, obj)
	if err != nil {
		nt.T.Fatalf("failed to parse test file: %v", err)
	}
	return obj
}
