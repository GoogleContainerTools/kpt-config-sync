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
	"time"

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
	e2eretry "kpt.dev/configsync/e2e/nomostest/retry"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
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
	namespace1 := k8sobjects.NamespaceObject(namespace1NN.Name)
	nt.Must(rootRepo.Add(nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name), namespace1))

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	deployment1 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	nt.Must(rootRepo.Add(deployment1Path, deployment1))
	nt.Must(rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
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
	rootSync := k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
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
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []testpredicates.Predicate{
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.MissingFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Delete the RootSync
	err = nt.KubeClient.Delete(rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}

	// The RootSync should skip finalizing and be deleted immediately
	err = nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace())
	if err != nil {
		nt.T.Fatal(err)
	}
	tg := taskgroup.New()
	tg.Go(func() error {
		// Namespace1 should NOT have been deleted, because it was orphaned by the RootSync.
		return nt.Watcher.WatchObject(kinds.Namespace(), namespace1.GetName(), namespace1.GetNamespace(),
			[]testpredicates.Predicate{testpredicates.HasAllNomosMetadata()}) // metadata NOT removed when orphaned
	})
	tg.Go(func() error {
		// Deployment1 should NOT have been deleted, because it was orphaned by the RootSync.
		return nt.Watcher.WatchObject(kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace(),
			[]testpredicates.Predicate{testpredicates.HasAllNomosMetadata()}) // metadata NOT removed when orphaned
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
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
	namespace1 := k8sobjects.NamespaceObject(namespace1NN.Name)
	nt.Must(rootRepo.Add(nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name), namespace1))

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	deployment1 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	nt.Must(rootRepo.Add(deployment1Path, deployment1))
	nt.Must(rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
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
	rootSync := k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
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
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []testpredicates.Predicate{
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Delete the RootSync
	nt.T.Log("Deleting RootSync")
	err = nt.KubeClient.Delete(rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}

	tg := taskgroup.New()
	// Deleting the RootSync should trigger the RootSync's finalizer to delete the Deployment1 and Namespace
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Namespace(), namespace1.GetName(), namespace1.GetNamespace())
	})
	tg.Go(func() error {
		// After Deployment1 is deleted, the RootSync should have its finalizer removed and be garbage collected
		return nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace())
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
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
	namespace1 := rootRepo.MustGet(nt.T, nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Namespace))

	// Add deployment-helloworld-1 to RootSync
	deployment1Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-1")
	deployment1 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment1.SetName(deployment1NN.Name)
	deployment1.SetNamespace(deployment1NN.Namespace)
	nt.Must(rootRepo.Add(deployment1Path, deployment1))
	nt.Must(rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Add deployment-helloworld-2 to RepoSync
	deployment2Path := nomostest.StructuredNSPath(deployment1NN.Namespace, "deployment-helloworld-2")
	deployment2 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment2.SetName(deployment2NN.Name)
	deployment2.SetNamespace(deployment1NN.Namespace)
	nt.Must(nsRepo.Add(deployment2Path, deployment2))
	nt.Must(nsRepo.CommitAndPush("Adding deployment helloworld-2 to RepoSync"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), deployment2.Name, deployment2.Namespace); err != nil {
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
	rootSync := k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
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
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []testpredicates.Predicate{
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	nt.T.Log("Enabling RepoSync deletion propagation")
	repoSync := rootRepo.MustGet(nt.T, repoSyncPath)
	if nomostest.SetDeletionPropagationPolicy(repoSync, metadata.DeletionPropagationPolicyForeground) {
		nt.Must(rootRepo.Add(repoSyncPath, repoSync))
		nt.Must(rootRepo.CommitAndPush("Enabling RepoSync deletion propagation"))
	}
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	require.NoError(nt.T,
		nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace(), []testpredicates.Predicate{
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Delete the RootSync
	err = nt.KubeClient.Delete(rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}

	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(), deployment2.GetName(), deployment2.GetNamespace())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Namespace(), namespace1.GetName(), namespace1.GetNamespace())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace())
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
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
	nt.Must(rootRepo.Add(deployment1Path, deployment1))
	nt.Must(rootRepo.CommitAndPush("Adding deployment helloworld-1 to RootSync"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), deployment1.Name, deployment1.Namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Add deployment-helloworld-2 to RepoSync
	deployment2Path := nomostest.StructuredNSPath(deployment2NN.Namespace, "deployment-helloworld-2")
	deployment2 := loadDeployment(nt, "../testdata/deployment-helloworld.yaml")
	deployment2.SetName(deployment2NN.Name)
	deployment2.SetNamespace(deployment2NN.Namespace)
	nt.Must(nsRepo.Add(deployment2Path, deployment2))
	nt.Must(nsRepo.CommitAndPush("Adding deployment helloworld-2 to RepoSync"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), deployment2.Name, deployment2.Namespace); err != nil {
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
	rootSync := k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
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
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace(), []testpredicates.Predicate{
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.HasFinalizer(metadata.ReconcilerFinalizer),
		}))

	nt.T.Log("Disabling RepoSync deletion propagation")
	repoSync := rootRepo.MustGet(nt.T, repoSyncPath)
	if nomostest.RemoveDeletionPropagationPolicy(repoSync) {
		nt.Must(rootRepo.Add(repoSyncPath, repoSync))
		nt.Must(rootRepo.CommitAndPush("Disabling RepoSync deletion propagation"))
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}
	}
	require.NoError(nt.T,
		nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace(), []testpredicates.Predicate{
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			testpredicates.MissingFinalizer(metadata.ReconcilerFinalizer),
		}))

	// Abandon the test namespace, otherwise it will block the finalizer
	namespace1Path := nomostest.StructuredNSPath(namespace1NN.Name, namespace1NN.Name)
	namespace1 := rootRepo.MustGet(nt.T, namespace1Path)
	core.SetAnnotation(namespace1, common.LifecycleDeleteAnnotation, common.PreventDeletion)
	nt.Must(rootRepo.Add(namespace1Path, namespace1))
	nt.Must(rootRepo.CommitAndPush("Adding annotation to keep test namespace on removal from git"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Delete the RootSync
	err = nt.KubeClient.Delete(rootSync)
	if err != nil {
		nt.T.Fatal(err)
	}

	tg := taskgroup.New()
	tg.Go(func() error {
		// Deleting the RootSync should trigger the RootSync's finalizer to delete the RepoSync and Deployment1
		return nt.Watcher.WatchForNotFound(kinds.Deployment(), deployment1.GetName(), deployment1.GetNamespace())
	})
	tg.Go(func() error {
		// RepoSync should delete quickly without finalizing
		return nt.Watcher.WatchForNotFound(kinds.RepoSyncV1Beta1(), repoSync.GetName(), repoSync.GetNamespace())
	})
	tg.Go(func() error {
		// After RepoSync and Deployment1 are deleted, the RootSync should have its finalizer removed and be garbage collected
		return nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), rootSync.GetName(), rootSync.GetNamespace())
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
	tg = taskgroup.New()
	tg.Go(func() error {
		// Namespace1 should NOT have been deleted, because it was abandoned by the RootSync.
		// TODO: Use NoConfigSyncMetadata predicate once metadata removal is fixed (b/256043590)
		return nt.Watcher.WatchObject(kinds.Namespace(), namespace1.GetName(), namespace1.GetNamespace(),
			[]testpredicates.Predicate{})
	})
	tg.Go(func() error {
		// Deployment2 should NOT have been deleted, because it was orphaned by the RepoSync.
		return nt.Watcher.WatchObject(kinds.Deployment(), deployment2.GetName(), deployment2.GetNamespace(),
			[]testpredicates.Predicate{testpredicates.HasAllNomosMetadata()})
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

// TestReconcileFinalizerReconcileTimeout verifies that the reconciler finalizer
// blocks deletion and continues to track objects which fail garbage collection.
// A RootSync is created which manages a single Namespace, and a fake finalizer
// is added to that Namespace from the test. Deletion of both the Namespace and
// RootSync should be blocked until the Namespace finalizer is removed.
func TestReconcileFinalizerReconcileTimeout(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nestedRootSyncNN := nomostest.RootSyncNN("nested-root-sync")
	namespaceNN := types.NamespacedName{Name: "managed-ns"}
	contrivedFinalizer := "e2e-test"
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.Unstructured,
		ntopts.RootRepo(nestedRootSyncNN.Name),      // Create a nested RootSync to delete mid-test
		ntopts.WithCentralizedControl,               // This test assumes centralized control
		ntopts.WithReconcileTimeout(10*time.Second), // Reconcile expected to fail, so use a short timeout
	)
	// add a Namespace to the nested RootSync
	namespace := k8sobjects.NamespaceObject(namespaceNN.Name)
	nsPath := nomostest.StructuredNSPath(namespace.GetNamespace(), namespaceNN.Name)
	nt.Must(nt.RootRepos[nestedRootSyncNN.Name].Add(nsPath, namespace))
	nt.Must(nt.RootRepos[nestedRootSyncNN.Name].CommitAndPush(fmt.Sprintf("add Namespace %s", namespaceNN.Name)))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Cleanup(func() {
		_, err := e2eretry.Retry(30*time.Second, func() error {
			namespace := &corev1.Namespace{}
			if err := nt.KubeClient.Get(namespaceNN.Name, namespaceNN.Namespace, namespace); err != nil {
				if apierrors.IsNotFound(err) { // Happy path - exit
					return nil
				}
				return err // unexpected error
			}
			if testutils.RemoveFinalizer(namespace, nomostest.ConfigSyncE2EFinalizer) {
				// The test failed to remove the finalizer. Remove to enable deletion.
				if err := nt.KubeClient.Update(namespace); err != nil {
					return err
				}
				nt.T.Log("removed finalizer in test cleanup")
			}
			return nil
		})
		if err != nil {
			nt.T.Fatal(err)
		}
	})
	// Add a fake finalizer to the namespace to block deletion
	testutils.AppendFinalizer(namespace, nomostest.ConfigSyncE2EFinalizer)
	nt.T.Logf("Add a fake finalizer named %s to Namespace %s", contrivedFinalizer, namespaceNN.Name)
	if err := nt.KubeClient.Apply(namespace); err != nil {
		nt.T.Fatal(err)
	}

	// Remove the fake finalizer
	t.Cleanup(func() {
		namespace = k8sobjects.NamespaceObject(namespaceNN.Name)
		nt.T.Logf("Remove the fake finalizer named %s from Namespace %s", contrivedFinalizer, namespaceNN.Name)
		if err := nt.KubeClient.Apply(namespace); err != nil {
			nt.T.Fatal(err)
		}
		// With the finalizer removed, the deletion should reconcile
		tg := taskgroup.New()
		tg.Go(func() error {
			return nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(),
				nestedRootSyncNN.Name, nestedRootSyncNN.Namespace)
		})
		tg.Go(func() error {
			return nt.Watcher.WatchForNotFound(kinds.Namespace(),
				namespaceNN.Name, namespaceNN.Namespace)
		})
		if err := tg.Wait(); err != nil {
			nt.T.Fatal(err)
		}
	})

	// Try to remove the nested RootSync. Deletion should be blocked by ns finalizer
	nestedRootSyncPath := fmt.Sprintf("acme/namespaces/%s/%s.yaml",
		configsync.ControllerNamespace, nestedRootSyncNN.Name)
	nt.Must(nt.RootRepos[rootSyncNN.Name].Remove(nestedRootSyncPath))
	nt.Must(nt.RootRepos[rootSyncNN.Name].CommitAndPush(fmt.Sprintf("remove Namespace %s", namespaceNN.Name)))
	expectedCondition := &v1beta1.RootSyncCondition{
		Type:    v1beta1.RootSyncReconcilerFinalizerFailure,
		Status:  "True",
		Reason:  "DestroyFailure",
		Message: "Failed to delete managed resource objects",
		Errors: []v1beta1.ConfigSyncError{
			{
				Code:         applier.ApplierErrorCode,
				ErrorMessage: "KNV2009: failed to wait for Namespace, /managed-ns: reconcile timeout\n\nFor more information, see https://g.co/cloud/acm-errors#knv2009",
			},
		},
	}
	// Finalizer currently only sets condition, not sync status
	if err := nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), nestedRootSyncNN.Name, nestedRootSyncNN.Namespace,
		[]testpredicates.Predicate{testpredicates.RootSyncHasCondition(expectedCondition)}); err != nil {
		nt.T.Fatal(err)
	}
	// Wait a fixed duration for RootSync deletion to be blocked
	time.Sleep(30 * time.Second)
	if err := nt.Validate(nestedRootSyncNN.Name, nestedRootSyncNN.Namespace, &v1beta1.RootSync{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(namespaceNN.Name, namespaceNN.Namespace, &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}
}

func cleanupSingleLevel(nt *nomostest.NT,
	rootSyncNN,
	deployment1NN,
	namespace1NN, safetyNamespace1NN types.NamespacedName,
) {
	cleanupSyncsAndObjects(nt,
		[]client.Object{
			k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name),
		},
		[]client.Object{
			k8sobjects.DeploymentObject(core.Name(deployment1NN.Name), core.Namespace(deployment1NN.Namespace)),
			k8sobjects.NamespaceObject(namespace1NN.Name),
			k8sobjects.NamespaceObject(safetyNamespace1NN.Name),
		})
}

func cleanupMultiLevel(nt *nomostest.NT,
	rootSyncNN, repoSyncNN,
	deployment1NN, deployment2NN,
	namespace1NN, safetyNamespace1NN, safetyNamespace2NN types.NamespacedName,
) {
	cleanupSyncsAndObjects(nt,
		[]client.Object{
			k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name),
			k8sobjects.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name),
		},
		[]client.Object{
			k8sobjects.DeploymentObject(core.Name(deployment1NN.Name), core.Namespace(deployment1NN.Namespace)),
			k8sobjects.DeploymentObject(core.Name(deployment2NN.Name), core.Namespace(deployment2NN.Namespace)),
			k8sobjects.NamespaceObject(namespace1NN.Name),
			k8sobjects.NamespaceObject(safetyNamespace1NN.Name),
			k8sobjects.NamespaceObject(safetyNamespace2NN.Name),
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

	tg := taskgroup.New()
	for _, syncObj := range syncObjs {
		o := syncObj
		tg.Go(func() error {
			return watchForNotFoundFromObject(nt, o)
		})
	}
	for _, obj := range objs {
		o := obj
		tg.Go(func() error {
			return watchForNotFoundFromObject(nt, o)
		})
	}
	if err := tg.Wait(); err != nil {
		nt.T.Error(err)
	}
}

// watchForNotFoundFromObject wraps WatchForNotFound, but allows the object
// to be fully populated, constructing a new empty typed object as needed.
func watchForNotFoundFromObject(nt *nomostest.NT, obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	scheme := nt.KubeClient.Client.Scheme()
	gvk, err := kinds.Lookup(obj, scheme)
	if err != nil {
		return err
	}
	return nt.Watcher.WatchForNotFound(gvk, key.Name, key.Namespace)
}

func newEmptyTypedObject(nt *nomostest.NT, obj client.Object) (client.Object, schema.GroupVersionKind, error) {
	scheme := nt.KubeClient.Client.Scheme()
	gvk, err := kinds.Lookup(obj, scheme)
	if err != nil {
		return nil, gvk, err
	}
	cObj, err := kinds.NewClientObjectForGVK(gvk, scheme)
	if err != nil {
		return nil, gvk, err
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
	err = nt.KubeClient.Delete(obj)
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
