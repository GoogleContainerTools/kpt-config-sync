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
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
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

	rootReconcilerPod, err := nt.KubeClient.GetDeploymentPod(
		nomostest.DefaultRootReconcilerName, configmanagement.ControllerNamespace,
		nt.DefaultWaitTimeout)
	if err != nil {
		nt.T.Fatal(err)
	}

	commitHash := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootReconcilerPod.Name, commitHash, metrics.ErrorSummary{
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
