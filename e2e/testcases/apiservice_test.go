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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

const (
	adapterNamespace = "custom-metrics"
	adapterName      = "custom-metrics-stackdriver-adapter"
)

func TestCreateAPIServiceAndEndpointInTheSameCommit(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.RequireGKE(t))
	t.Cleanup(func() {
		if t.Failed() {
			nt.PodLogs(adapterNamespace, adapterName, "pod-custom-metrics-stackdriver-adapter", true)
		}
	})
	nt.T.Log("Creating commit with APIService and Deployment")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/namespace.yaml", "acme/namespaces/custom-metrics/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/namespace-custom-metrics.yaml", "acme/namespaces/custom-metrics/namespace-custom-metrics.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/rbac.yaml", "acme/cluster/rbac.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/apiservice.yaml", "acme/cluster/apiservice.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("adding apiservice resources")
	nt.T.Log("Waiting for nomos to sync new APIService")
	nt.WaitForRepoSyncs()

	err := validateStackdriverAdapterStatusCurrent(nt)
	if err != nil {
		nt.T.Fatalf("validate failed %s", err)
	}

	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/apiservice.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove custom metric stackdriver adapter API service")
	nt.WaitForRepoSyncs()

	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/custom-metrics/namespace-custom-metrics.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove custom metric stackdriver adapter deployment")
	nt.WaitForRepoSyncs()

	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/custom-metrics/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove custom metric stackdriver adapter namespace")
	nt.WaitForRepoSyncs()
}

func TestImporterAndSyncerResilientToFlakyAPIService(t *testing.T) {
	nt := nomostest.New(t, ntopts.RequireGKE(t), ntopts.Unstructured)
	nt.T.Log("Adding API service with backend temporarily unavailable")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/apiservice.yaml")
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "-f", "../testdata/apiservice/apiservice.yaml", "--ignore-not-found")
		if t.Failed() {
			nt.PodLogs(adapterNamespace, adapterName, "pod-custom-metrics-stackdriver-adapter", true)
		}
	})

	nt.T.Log("Creating commit with resources")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/rbac.yaml", "acme/cluster/rbac.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add testing resources")

	nt.T.Log("Adding backend")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/namespace.yaml")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/namespace-custom-metrics.yaml")
	nt.T.Log("Waiting for nomos to stabilize")
	nt.WaitForRepoSyncs()
	err := validateStackdriverAdapterStatusCurrent(nt)
	if err != nil {
		nt.T.Fatalf("validate failed %s", err)
	}
}

func validateStackdriverAdapterStatusCurrent(nt *nomostest.NT) error {
	// Add retry for backward compatibility for mono repo mode as the status update
	// does not wait for reconciliation
	t, e := nomostest.Retry(nt.DefaultReconcileTimeout, func() error {
		var err error
		err = nt.Validate("custom-metrics-stackdriver-adapter", "custom-metrics", &corev1.Service{},
			nomostest.StatusEquals(nt, status.CurrentStatus))
		if err != nil {
			return err
		}
		err = nt.Validate("custom-metrics-stackdriver-adapter", "custom-metrics", &appsv1.Deployment{},
			nomostest.StatusEquals(nt, status.CurrentStatus))
		if err != nil {
			return err
		}
		err = nt.Validate("external-metrics-reader", "custom-metrics", &rbacv1.ClusterRole{},
			nomostest.StatusEquals(nt, status.CurrentStatus))
		if err != nil {
			return err
		}
		err = nt.Validate("custom-metrics-auth-reader", "custom-metrics", &rbacv1.RoleBinding{},
			nomostest.StatusEquals(nt, status.CurrentStatus))
		if err != nil {
			return err
		}
		err = nt.Validate("custom-metrics-stackdriver-adapter", "custom-metrics", &corev1.ServiceAccount{},
			nomostest.StatusEquals(nt, status.CurrentStatus))
		if err != nil {
			return err
		}
		err = nt.Validate("custom-metrics", "", &corev1.Namespace{},
			nomostest.StatusEquals(nt, status.CurrentStatus))
		return err
	})
	nt.T.Logf("took %v to wait for custom metrics stackdriver adapter", t)
	return e
}
