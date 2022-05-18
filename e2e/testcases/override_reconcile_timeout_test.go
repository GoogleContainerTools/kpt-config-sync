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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestOverrideReconcileTimeout tests that a misconfigured pod will never reconcile (timeout).
func TestOverrideReconcileTimeout(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.SkipMonoRepo)
	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)

	// Override reconcileTimeout to a short time 30s, pod will never be ready.
	// Only actuation should succeed, reconcile should time out.
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"reconcileTimeout": "30s"}}}`)
	nt.WaitForRepoSyncs()
	namespaceName := "timeout-1"

	pod1Name := "pod-1"
	container := corev1.Container{
		Name:  "goproxy",
		Image: "k8s.gcr.io/goproxy:0.1",
		Ports: []corev1.ContainerPort{
			{
				ContainerPort: 8080,
			},
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(8081), // wrong port will never be ready
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
		},
	}
	pod1 := fake.PodObject(pod1Name, []corev1.Container{container}, core.Namespace(namespaceName))
	idToVerify := core.IDOf(pod1)

	nt.RootRepos[configsync.RootSyncName].Add("acme/pod-1.yaml", pod1)
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns-1.yaml", fake.NamespaceObject(namespaceName))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Add namespace/%s & pod/%s (never ready)", namespaceName, pod1Name))
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
		err = verifyObjectStatus(u, idToVerify, expectActuationStatus, expectReconcileStatus)
		if err != nil {
			nt.T.Fatal(err)
		} else {
			nt.T.Logf("object %q has actuation and reconcile status as expected", namespaceName)
		}
	}

	nt.RootRepos[configsync.RootSyncName].Remove("acme/pod-1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Remove pod/%s (never ready)", pod1Name))
	nt.WaitForRepoSyncs()

	// Verify pod is deleted.
	// Otherwise deletion of a managed resource will be denied by the webhook.
	nt.MustKubectl("delete", "pods", pod1Name, "-n", namespaceName, "--ignore-not-found")

	// Override reconcileTimeout to 5m, namespace actuation should succeed, namespace reconcile should succeed.
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"reconcileTimeout": "5m"}}}`)
	nt.WaitForRepoSyncs()

	// Fix ReadinessProbe to use the right port
	pod1.Spec.Containers[0].ReadinessProbe.ProbeHandler.TCPSocket.Port =
		intstr.FromInt(int(pod1.Spec.Containers[0].Ports[0].ContainerPort))

	nt.RootRepos[configsync.RootSyncName].Add("acme/pod-1.yaml", pod1)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Add pod/%s (fixed)", pod1Name))
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
		err = verifyObjectStatus(u, idToVerify, expectActuationStatus, expectReconcileStatus)
		if err != nil {
			nt.T.Fatal(err)
		} else {
			nt.T.Logf("object %q has actuation and reconcile status as expected", namespaceName)
		}
	}
}

// verifyObjectStatus verifies that an object has actuation and reconcile status as expected
func verifyObjectStatus(u *unstructured.Unstructured, id core.ID, expectActuation string, expectReconcile string) error {
	resourceStatuses, exist, err := unstructured.NestedSlice(u.Object, "status", "resourceStatuses")
	if !exist {
		return fmt.Errorf("failed to find status.resourceStatues field")
	}
	if err != nil {
		return fmt.Errorf("failed to get resourceStatues: %v", err)
	}
	for _, r := range resourceStatuses {
		resourceStatus := r.(interface{}).(map[string]interface{})
		if resourceStatus["name"] == id.Name && resourceStatus["kind"] == id.Kind &&
			resourceStatus["namespace"] == id.Namespace && resourceStatus["group"] == id.Group {
			if resourceStatus["actuation"] == expectActuation && resourceStatus["reconcile"] == expectReconcile {
				return nil
			}
			return fmt.Errorf("object %q does not have expected actuation and reconcile status, actual actuation status is %q, expect %q; actual reconcile status is %q, expect %q",
				resourceStatus["name"], resourceStatus["actuation"], expectActuation, resourceStatus["reconcile"], expectReconcile)
		}
	}
	return fmt.Errorf("failed to find object %s : %s", id.Kind, id.Name)
}
