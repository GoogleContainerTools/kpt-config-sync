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
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type resourceInfo struct {
	group     string
	name      string
	kind      string
	namespace string
}

// TestOverrideReconcileTimeout try to create a namespace through Config Sync with short reconcileTimeout(1s) and default ReconcileTimeout(5m)
func TestOverrideReconcileTimeout(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.SkipMonoRepo)
	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)

	// Override reconcileTimeout to a very short time 1s, namespace reconcile can not finish in 1s.
	// only actuation should succeed, reconcile should time out
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"reconcileTimeout": "1s"}}}`)
	nt.WaitForRepoSyncs()
	namespaceName := "foo1"
	resourceToVerify := &resourceInfo{group: "", name: namespaceName, kind: "Namespace", namespace: ""}
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns-1.yaml", fake.NamespaceObject(namespaceName))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Add namespace %s", namespaceName))
	nt.WaitForRepoSyncs()

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "kpt.dev",
		Kind:    "ResourceGroup",
		Version: "v1alpha1",
	})

	err := nt.Client.Get(nt.Context, client.ObjectKey{
		Namespace: "config-management-system",
		Name:      "root-sync",
	}, u)
	if err != nil {
		nt.T.Fatal(err)
	} else {
		expectActuationStatus := "Succeeded"
		expectReconcileStatus := "Timeout"
		err = verifyResourceStatus(u, resourceToVerify, expectActuationStatus, expectReconcileStatus)
		if err != nil {
			nt.T.Fatal(err)
		} else {
			nt.T.Logf("resource %q has actuation and reconcile status as expected", namespaceName)
		}
	}

	nt.RootRepos[configsync.RootSyncName].Remove("acme/ns-1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Remove namespace %s", namespaceName))
	nt.WaitForRepoSyncs()
	nt.MustKubectl("delete", "namespaces", namespaceName, "--ignore-not-found")

	// Override reconcileTimeout to 5m, namespace actuation should succeed, namespace reconcile should succeed.
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"reconcileTimeout": "5m"}}}`)
	nt.WaitForRepoSyncs()
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns-1.yaml", fake.NamespaceObject(namespaceName))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Add namespace %s", namespaceName))
	nt.WaitForRepoSyncs()

	err = nt.Client.Get(nt.Context, client.ObjectKey{
		Namespace: "config-management-system",
		Name:      "root-sync",
	}, u)
	if err != nil {
		nt.T.Error(err)
	} else {
		expectActuationStatus := "Succeeded"
		expectReconcileStatus := "Succeeded"
		err = verifyResourceStatus(u, resourceToVerify, expectActuationStatus, expectReconcileStatus)
		if err != nil {
			nt.T.Fatal(err)
		} else {
			nt.T.Logf("resource %q has actuation and reconcile status as expected", namespaceName)
		}
	}
}

// verifyResourceStatus verify if a resource has actuation and reconcile status as expected
func verifyResourceStatus(u *unstructured.Unstructured, resource *resourceInfo, expectActuation string, expectReconcile string) error {
	resourceStatuses, exist, err := unstructured.NestedSlice(u.Object, "status", "resourceStatuses")
	if !exist {
		return fmt.Errorf("failed to find status.resourceStatues field")
	}
	if err != nil {
		return fmt.Errorf("failed to get resourceStatues: %v", err)
	}
	for _, r := range resourceStatuses {
		resourceStatus := r.(interface{}).(map[string]interface{})
		if resourceStatus["name"] == resource.name && resourceStatus["kind"] == resource.kind &&
			resourceStatus["namespace"] == resource.namespace && resourceStatus["group"] == resource.group {
			if resourceStatus["actuation"] == expectActuation && resourceStatus["reconcile"] == expectReconcile {
				return nil
			}
			return fmt.Errorf("resource %q does not have expected actuation and reconcile status, actual actuation status is %q, expect %q; actual reconcile status is %q, expect %q",
				resourceStatus["name"], resourceStatus["actuation"], expectActuation, resourceStatus["reconcile"], expectReconcile)
		}
	}
	return fmt.Errorf("failed to find resource %s : %s", resource.kind, resource.name)
}
