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
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestApplyScopedResourcesHierarchicalMode(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipAutopilotCluster)

	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces")
	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/kubevirt/.", "acme")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add kubevirt configs")

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			out, err := nt.Kubectl("get", "service", "-n", "kubevirt")
			// Print a standardized header before each printed log to make ctrl+F-ing the
			// log you want easier.
			nt.T.Logf("kubectl get service -n kubevirt: \n%s", string(out))
			if err != nil {
				nt.T.Log("error running `kubectl get service -n kubevirt`:", err)
			}
		}
		// Avoids KNV2010 error since the bookstore namespace contains a VM custom resource
		// KNV2010: unable to apply resource: the server could not find the requested resource (patch virtualmachines.kubevirt.io testvm)
		// Error occurs semi-consistently (~50% of the time) with the CI mono-repo kind tests
		nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/bookstore1")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove bookstore1 namespace")
		nt.WaitForRepoSyncs()

		// kubevirt must be removed separately to allow the custom resource to be deleted
		nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/kubevirt/kubevirt-cr.yaml")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove kubevirt custom resource")
		nt.WaitForRepoSyncs()

		// Wait for the kubevirt custom resource to be deleted to prevent the custom resource from
		// being stuck in the Terminating state which can occur if the operator is deleted prior
		// to the resource.
		waitForKubeVirtDeletion(nt)

		// Avoids KNV2006 since the repo contains a number of cluster scoped resources
		// https://cloud.google.com/anthos-config-management/docs/reference/errors#knv2006
		nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/kubevirt-operator-cluster-role.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/kubevirt.io:operator-clusterrole.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/kubevirt-cluster-critical.yaml")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove cluster roles and priority class")
		nt.WaitForRepoSyncs()
	})

	nt.WaitForRepoSyncs(nomostest.WithTimeout(7 * time.Minute))

	err := nomostest.WaitForCRDs(nt, []string{"virtualmachines.kubevirt.io"})
	if err != nil {
		nt.T.Fatal(err)
	}

	_, err = nomostest.Retry(60*time.Second, func() error {
		_, err := nt.Kubectl("get", "vm", "testvm", "-n", "bookstore1")
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	_, err = nomostest.Retry(60*time.Second, func() error {
		return nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
			err := nt.ValidateMetricNotFound(ocmetrics.ReconcilerErrorsView.Name)
			if err != nil {
				return err
			}
			return nil
		})
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestApplyScopedResourcesUnstructuredMode(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.SkipAutopilotCluster)

	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/kubevirt-compiled/.", "acme")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add kubevirt configs")

	nt.T.Cleanup(func() {
		if nt.T.Failed() {
			out, err := nt.Kubectl("get", "service", "-n", "kubevirt")
			// Print a standardized header before each printed log to make ctrl+F-ing the
			// log you want easier.
			nt.T.Logf("kubectl get service -n kubevirt: \n%s", string(out))
			if err != nil {
				nt.T.Log("error running `kubectl get service -n kubevirt`:", err)
			}
		}
		// Avoids KNV2010 error since the bookstore namespace contains a VM custom resource
		// KNV2010: unable to apply resource: the server could not find the requested resource (patch virtualmachines.kubevirt.io testvm)
		// Error occurs semi-consistently (~50% of the time) with the CI mono-repo kind tests
		nt.RootRepos[configsync.RootSyncName].Remove("acme/namespace_bookstore1.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/bookstore1")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove bookstore1 namespace")
		nt.WaitForRepoSyncs()

		// kubevirt must be removed separately to allow the custom resource to be deleted
		nt.RootRepos[configsync.RootSyncName].Remove("acme/kubevirt/kubevirt_kubevirt.yaml")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove kubevirt custom resource")
		nt.WaitForRepoSyncs()

		// Wait for the kubevirt custom resource to be deleted to prevent the custom resource from
		// being stuck in the Terminating state which can occur if the operator is deleted prior
		// to the resource.
		waitForKubeVirtDeletion(nt)

		// Avoids KNV2006 since the repo contains a number of cluster scoped resources
		// https://cloud.google.com/anthos-config-management/docs/reference/errors#knv2006
		nt.RootRepos[configsync.RootSyncName].Remove("acme/clusterrole_kubevirt-operator.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/clusterrole_kubevirt.io:operator.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/clusterrolebinding_kubevirt-operator.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/priorityclass_kubevirt-cluster-critical.yaml")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove cluster roles and priority class")
		nt.WaitForRepoSyncs()
	})

	nt.WaitForRepoSyncs(nomostest.WithTimeout(7 * time.Minute))

	err := nomostest.WaitForCRDs(nt, []string{"virtualmachines.kubevirt.io"})
	if err != nil {
		nt.T.Fatal(err)
	}

	_, err = nomostest.Retry(60*time.Second, func() error {
		_, err := nt.Kubectl("get", "vm", "testvm", "-n", "bookstore1")
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	_, err = nomostest.Retry(60*time.Second, func() error {
		return nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
			err := nt.ValidateMetricNotFound(ocmetrics.ReconcilerErrorsView.Name)
			if err != nil {
				return err
			}
			return nil
		})
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func waitForKubeVirtDeletion(nt *nomostest.NT) {
	_, err := nomostest.Retry(30*time.Second, func() error {
		return nt.ValidateNotFound("kubevirt", "kubevirt", kubeVirtObject())
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func kubeVirtObject() client.Object {
	kubeVirtObj := &unstructured.Unstructured{}
	kubeVirtObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kubevirt.io",
		Version: "v1",
		Kind:    "kubevirt",
	})

	return kubeVirtObj
}
