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

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestApplyScopedResources(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.SkipAutopilotCluster)

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

		// Use Retry & ValidateNotFound instead of WatchForNotFound, because
		// watching would require importing the KubeVirt API objects.
		_, err := nomostest.Retry(30*time.Second, func() error {
			return nt.ValidateNotFound("kubevirt", "kubevirt", kubeVirtObject())
		})
		if err != nil {
			nt.T.Error(err)
		}

		// Avoids KNV2006 since the repo contains a number of cluster scoped resources
		// https://cloud.google.com/anthos-config-management/docs/reference/errors#knv2006
		nt.RootRepos[configsync.RootSyncName].Remove("acme/clusterrole_kubevirt-operator.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/clusterrole_kubevirt.io:operator.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/clusterrolebinding_kubevirt-operator.yaml")
		nt.RootRepos[configsync.RootSyncName].Remove("acme/priorityclass_kubevirt-cluster-critical.yaml")
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove cluster roles and priority class")
		nt.WaitForRepoSyncs()
	})

	// The example includes the virt-operator deployment, which installs the VirtualMachine CRD.
	// The example also includes a VirtualMachine, which will be skipped until the CRD is applied.
	nt.WaitForRepoSyncs(nomostest.WithTimeout(nt.DefaultWaitTimeout * 2))

	// The VirtualMachine CRD should already be reconciled, because if not the
	// VirtualMachine object wouldn't have been applied, and WaitForRepoSyncs
	// would have failed.
	err := nt.Validate("virtualmachines.kubevirt.io", "", &apiextensionsv1.CustomResourceDefinition{},
		nomostest.IsEstablished)
	if err != nil {
		nt.T.Fatal(err)
	}

	// The VirtualMachine should already be applied, but it may or may not ever
	// reconcile. KubeVirt does nested virtualization, which requires certain
	// compute instance types, which may not be satisfied by all test clusters.
	// https://kubevirt.io/quickstart_cloud/
	// https://cloud.google.com/compute/docs/instances/nested-virtualization/overview#restrictions
	err = nt.Validate("testvm", "bookstore1", virtualMachineObject())
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
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
		Kind:    "KubeVirt",
	})
	return kubeVirtObj
}

func virtualMachineObject() client.Object {
	kubeVirtObj := &unstructured.Unstructured{}
	kubeVirtObj.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kubevirt.io",
		Version: "v1",
		Kind:    "VirtualMachine",
	})
	return kubeVirtObj
}
