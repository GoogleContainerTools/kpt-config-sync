// Copyright 2023 Google LLC
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

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSurfaceFightErrorForUpdate(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl)

	nt.T.Logf("Stop the admission webhook to generate the fights")
	nomostest.StopWebhook(nt)

	ns := fake.NamespaceObject("test-ns", core.Annotation("foo", "bar"))
	rb := roleBinding("test-rb", ns.Name, map[string]string{"foo": "bar"})
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/ns.yaml", ns.Name), ns))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/rb.yaml", ns.Name), rb))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add Namespace and RoleBinding"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Simulate update fight")
	// Make the # of updates exceed the fightThreshold defined in pkg/syncer/reconcile/fight/detector.go
	for i := 0; i <= 5; i++ {
		nt.MustMergePatch(ns, `{"metadata": {"annotations": {"foo": "baz"}}}`)
		nt.MustMergePatch(rb, `{"metadata": {"annotations": {"foo": "baz"}}}`)
		// Wait for the Namespace change to be reverted by the Remediator before the next update
		require.NoError(nt.T,
			nt.Watcher.WatchObject(kinds.Namespace(), ns.GetName(), "", []testpredicates.Predicate{
				testpredicates.HasAnnotation("foo", "bar"),
			}))
		// Wait for the RoleBinding change to be reverted by the Remediator before the next update
		require.NoError(nt.T,
			nt.Watcher.WatchObject(kinds.RoleBinding(), rb.GetName(), rb.GetNamespace(), []testpredicates.Predicate{
				testpredicates.HasAnnotation("foo", "bar"),
			}))
	}

	nt.T.Log("The RootSync reports a fight error")
	nt.WaitForRootSyncSyncError(configsync.RootSyncName, status.FightErrorCode,
		"This may indicate Config Sync is fighting with another controller over the object.")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			// At least 2 fights, one for the Namespace and one for the RoleBinding.
			// There might be more fights being recording while it is cooling down, but it is not guaranteed.
			Fights: 2,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("The fight error should be auto-resolved if no more fights")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestSurfaceFightErrorForDelete(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl)

	nt.T.Logf("Stop the admission webhook to generate the fights")
	nomostest.StopWebhook(nt)

	ns := fake.NamespaceObject("test-ns", core.Annotation("foo", "bar"))
	rb := roleBinding("test-rb", ns.Name, map[string]string{"foo": "bar"})
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/ns.yaml", ns.Name), ns))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/rb.yaml", ns.Name), rb))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add Namespace and RoleBinding"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Simulate delete fight")
	// Make the # of updates exceed the fightThreshold defined in pkg/syncer/reconcile/fight/detector.go
	for i := 0; i <= 5; i++ {
		if err := nt.KubeClient.Delete(rb, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
		// Wait for the RoleBindign to be recreated by the Remediator before the next delete.
		require.NoError(nt.T, nt.Watcher.WatchForCurrentStatus(kinds.RoleBinding(), rb.GetName(), rb.GetNamespace()))
	}

	nt.T.Log("The RootSync reports a fight error")
	nt.WaitForRootSyncSyncError(configsync.RootSyncName, status.FightErrorCode,
		"This may indicate Config Sync is fighting with another controller over the object.")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Fights: 1, // At least one fight for the RoleBinding
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("The fight error should be auto-resolved if no more fights")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestSurfaceFightErrorForCreate(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl)

	nt.T.Logf("Stop the admission webhook to generate the fights")
	nomostest.StopWebhook(nt)

	ns := fake.NamespaceObject("test-ns", core.Annotation("foo", "bar"))
	rb := roleBinding("test-rb", ns.Name, map[string]string{"foo": "bar"})
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/ns.yaml", ns.Name), ns))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/rb.yaml", ns.Name), rb))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add Namespace and RoleBinding"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Simulate create fight")
	// Make the # of updates exceed the fightThreshold defined in pkg/syncer/reconcile/fight/detector.go
	for i := 0; i <= 5; i++ {
		newNs := fake.NamespaceObject("test-ns1")
		newNs.SetAnnotations(map[string]string{
			// These are the minimal annotations to trigger a remediation.
			metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
			metadata.ResourceIDKey:         core.GKNN(newNs),
			metadata.ResourceManagerKey:    string(declared.RootReconciler),
		})
		// Wait for the Namespace to be deleted by the Remediator before the next create
		if err := nt.KubeClient.Create(newNs); err != nil && !apierrors.IsAlreadyExists(err) {
			nt.T.Error(err)
		}
		require.NoError(nt.T, nt.Watcher.WatchForNotFound(kinds.Namespace(), newNs.GetName(), ""))
	}

	nt.T.Log("The RootSync reports a fight error")
	nt.WaitForRootSyncSyncError(configsync.RootSyncName, status.FightErrorCode,
		"This may indicate Config Sync is fighting with another controller over the object.")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Fights: 1, // At least one fight for the new Namespace
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("The fight error should be auto-resolved if no more fights")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}
