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
	"testing"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/kinds"
)

const (
	adapterNamespace = "custom-metrics"
	adapterName      = "custom-metrics-stackdriver-adapter"
)

func TestCreateAPIServiceAndEndpointInTheSameCommit(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.RequireGKE(t))
	t.Cleanup(func() {
		if t.Failed() {
			nt.PodLogs(adapterNamespace, adapterName, "pod-custom-metrics-stackdriver-adapter", true)
		}
	})
	nt.T.Log("Creating commit with APIService and Deployment")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/rbac.yaml", "acme/cluster/rbac.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/namespace.yaml", "acme/namespaces/custom-metrics/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/namespace-custom-metrics.yaml", "acme/namespaces/custom-metrics/namespace-custom-metrics.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/apiservice.yaml", "acme/cluster/apiservice.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("adding apiservice resources")
	nt.T.Log("Waiting for nomos to sync new APIService")
	nt.WaitForRepoSyncs()

	err := validateStackdriverAdapterStatusCurrent(nt)
	if err != nil {
		nt.T.Fatalf("validate failed %s", err)
	}

	// Test done, removing the test APIService first to prevent Discovery failure blocking
	// the test repo from cleaning up
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/apiservice.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove custom metric stackdriver adapter API service")
	nt.WaitForRepoSyncs()

	// Remove the backend Deployment of test APIService
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/custom-metrics/namespace-custom-metrics.yaml")
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/custom-metrics/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove custom metric stackdriver adapter namespace")
	nt.WaitForRepoSyncs()
}

func TestImporterAndSyncerResilientToFlakyAPIService(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.RequireGKE(t), ntopts.Unstructured)
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "-f", "../testdata/apiservice/apiservice.yaml", "--ignore-not-found")
		nt.MustKubectl("delete", "-f", "../testdata/apiservice/namespace-custom-metrics.yaml", "--ignore-not-found")
		nt.MustKubectl("delete", "-f", "../testdata/apiservice/namespace.yaml", "--ignore-not-found")
		nt.MustKubectl("delete", "-f", "../testdata/apiservice/rbac.yaml", "--ignore-not-found")

		if t.Failed() {
			nt.PodLogs(adapterNamespace, adapterName, "pod-custom-metrics-stackdriver-adapter", true)
		}
	})

	nt.T.Log("Adding APIService with backend temporarily unavailable to trigger discovery errors")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/apiservice.yaml")

	nt.T.Log("Creating commit with test resources")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/namespace-resilient.yaml", "acme/namespaces/resilient/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add testing resources")

	nt.T.Log("Wait for test resource to have status CURRENT")
	nt.WaitForRepoSyncs()

	err := nomostest.WatchForCurrentStatus(nt, kinds.Namespace(), "resilient", "")
	if err != nil {
		nt.T.Fatal("validate failed test resource to have status CURRENT")
	}

	nt.T.Log("Adding backend to test APIService to bring Config Sync out of error state")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/namespace.yaml")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/rbac.yaml")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/namespace-custom-metrics.yaml")
	nt.T.Log("Waiting for nomos to stabilize")
	nt.WaitForRepoSyncs()

	err = validateStackdriverAdapterStatusCurrent(nt)
	if err != nil {
		nt.T.Fatalf("validate failed %s", err)
	}
}

func validateStackdriverAdapterStatusCurrent(nt *nomostest.NT) error {
	tg := taskgroup.New()
	tg.Go(func() error {
		return nomostest.WatchForCurrentStatus(nt, kinds.Service(), "custom-metrics-stackdriver-adapter", "custom-metrics")
	})
	tg.Go(func() error {
		return nomostest.WatchForCurrentStatus(nt, kinds.Deployment(), "custom-metrics-stackdriver-adapter", "custom-metrics")
	})
	tg.Go(func() error {
		return nomostest.WatchForCurrentStatus(nt, kinds.ClusterRole(), "external-metrics-reader", "custom-metrics")
	})
	tg.Go(func() error {
		return nomostest.WatchForCurrentStatus(nt, kinds.RoleBinding(), "custom-metrics-auth-reader", "custom-metrics")
	})
	tg.Go(func() error {
		return nomostest.WatchForCurrentStatus(nt, kinds.ServiceAccount(), "custom-metrics-stackdriver-adapter", "custom-metrics")
	})
	tg.Go(func() error {
		return nomostest.WatchForCurrentStatus(nt, kinds.Namespace(), "custom-metrics", "")
	})
	return tg.Wait()
}
