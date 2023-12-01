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
	"time"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestSurfaceFightError(t *testing.T) {
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

	// Make the # of updates exceed the fightThreshold defined in pkg/syncer/reconcile/fight_detector.go
	go func() {
		for i := 0; i <= 5; i++ {
			nt.MustMergePatch(ns, `{"metadata": {"annotations": {"foo": "baz"}}}`)
			nt.MustMergePatch(rb, `{"metadata": {"annotations": {"foo": "baz"}}}`)
			time.Sleep(time.Second)
		}
	}()

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
			Fights: 5,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("The fight error should be auto-resolved if no more fights")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestRemediateDrift(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl, ntopts.Unstructured)

	nt.T.Logf("Stop the admission webhook to allow drift")
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

	actualRB := fake.RoleBindingObject()
	if err := nt.KubeClient.Get(rb.Name, rb.Namespace, actualRB); err != nil {
		nt.T.Fatal(err)
	}
	// Remove managed-by label to ensure remediator can recover the label
	labels := actualRB.GetLabels()
	delete(labels, metadata.ManagedByKey)
	actualRB.SetLabels(labels)
	// Update a declared annotation to cause a resource conflict
	annotations := actualRB.GetAnnotations()
	annotations["foo"] = "baz"
	actualRB.SetAnnotations(annotations)
	nt.T.Log("Update the RoleBinding to create drift")
	if err := nt.KubeClient.Update(actualRB); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Wait for the drift to be remediated")
	err := nt.Watcher.WatchObject(kinds.RoleBinding(), rb.Name, rb.Namespace,
		[]testpredicates.Predicate{
			testpredicates.HasLabel(metadata.ManagedByKey, metadata.ManagedByValue),
			testpredicates.HasAnnotation("foo", "bar"),
		})
	if err != nil {
		nt.T.Fatal(err)
	}
}
