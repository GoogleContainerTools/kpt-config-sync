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
	"k8s.io/apimachinery/pkg/util/intstr"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	resourcegroupv1alpha1 "kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestOverrideReconcileTimeout tests that a misconfigured pod will never reconcile (timeout).
func TestOverrideReconcileTimeout(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	rootSync := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)

	// Override reconcileTimeout to a short time 30s, only actuation should succeed, reconcile should time out.
	if err := nt.KubeClient.MergePatch(rootSync,
		`{"spec": {"override": {"reconcileTimeout": "30s"}}}`); err != nil {
		nt.T.Fatal(err)
	}
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasObservedGenerationNoLessThan(rootSync.Generation),
		)))
	nt.Must(nt.WatchForAllSyncs())
	// Pre-provision a low priority workload to force the cluster to scale up.
	// Later, when the real workload is being scheduled, if there's no more resources available
	// (common on Autopilot clusters, which are optimized for utilization),
	// this low priority workload will be evicted to make room for the new normal priority workload.
	if nt.IsGKEAutopilot {
		nt.T.Cleanup(func() {
			nt.MustKubectl("delete", "-f", "../testdata/low-priority-pause-deployment.yaml", "--ignore-not-found")
		})
		nt.MustKubectl("apply", "-f", "../testdata/low-priority-pause-deployment.yaml")
		nt.Must(nt.Watcher.WatchObject(kinds.Deployment(), "pause-deployment", "default",
			testwatcher.WatchPredicates(
				testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus),
			)))
	}

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
					Port: intstr.FromInt(8080),
				},
			},
			// Make initial delay longer than current reconcile timeout 30s.
			// This will cause the applier to exit with a timeout,
			// but the workload will still become available afterwards
			InitialDelaySeconds: 3600,
			PeriodSeconds:       10,
		},
	}
	pod1 := k8sobjects.PodObject(pod1Name, []corev1.Container{container}, core.Namespace(namespaceName))
	idToVerify := core.IDOf(pod1)

	nt.Must(rootSyncGitRepo.Add("acme/pod-1.yaml", pod1))
	nt.Must(rootSyncGitRepo.Add("acme/ns-1.yaml", k8sobjects.NamespaceObject(namespaceName)))
	nt.Must(rootSyncGitRepo.CommitAndPush(fmt.Sprintf("Add namespace/%s & pod/%s (never ready)", namespaceName, pod1Name)))
	nt.Must(nt.WatchForAllSyncs())
	expectActuationStatus := "Succeeded"
	expectReconcileStatus := "Timeout"
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), "root-sync", "config-management-system",
		testwatcher.WatchPredicates(
			resourceStatusEquals(idToVerify, expectActuationStatus, expectReconcileStatus),
		)))

	// Override reconcileTimeout to 5m, namespace actuation should succeed, namespace reconcile should succeed.
	if err := nt.KubeClient.MergePatch(rootSync,
		`{"spec": {"override": {"reconcileTimeout": "5m"}}}`); err != nil {
		nt.T.Fatal(err)
	}
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			testpredicates.RootSyncHasObservedGenerationNoLessThan(rootSync.Generation),
		)))

	nt.Must(rootSyncGitRepo.Remove("acme/pod-1.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush(fmt.Sprintf("Remove pod/%s", pod1Name)))
	nt.Must(nt.WatchForAllSyncs())

	// Verify pod is deleted.
	if err := nt.ValidateNotFound(pod1Name, namespaceName, &corev1.Pod{}); err != nil {
		nt.T.Fatal(err)
	}
	pod1.Spec.Containers[0].ReadinessProbe.InitialDelaySeconds = 30
	nt.Must(rootSyncGitRepo.Add("acme/pod-1.yaml", pod1))
	nt.Must(rootSyncGitRepo.CommitAndPush(fmt.Sprintf("Add pod/%s (ready after 30s)", pod1Name)))
	nt.Must(nt.WatchForAllSyncs())

	expectActuationStatus = "Succeeded"
	expectReconcileStatus = "Succeeded"
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), "root-sync", "config-management-system",
		testwatcher.WatchPredicates(
			resourceStatusEquals(idToVerify, expectActuationStatus, expectReconcileStatus),
		)))
}

// resourceStatusEquals verifies that an object has actuation and reconcile status as expected
func resourceStatusEquals(id core.ID, expectActuation, expectReconcile string) testpredicates.Predicate {
	return func(obj client.Object) error {
		if obj == nil {
			return testpredicates.ErrObjectNotFound
		}
		rg, ok := obj.(*resourcegroupv1alpha1.ResourceGroup)
		if !ok {
			return testpredicates.WrongTypeErr(obj, &resourcegroupv1alpha1.ResourceGroup{})
		}
		resourceStatuses := rg.Status.ResourceStatuses
		for _, resourceStatus := range resourceStatuses {
			if resourceStatus.Name == id.Name && resourceStatus.Kind == id.Kind &&
				resourceStatus.Namespace == id.Namespace && resourceStatus.Group == id.Group {
				if string(resourceStatus.Actuation) == expectActuation && string(resourceStatus.Reconcile) == expectReconcile {
					return nil
				}
				return fmt.Errorf("object %q does not have expected actuation and reconcile status, actual actuation status is %q, expect %q; actual reconcile status is %q, expect %q",
					resourceStatus.Name, resourceStatus.Actuation, expectActuation, resourceStatus.Reconcile, expectReconcile)
			}
		}
		return fmt.Errorf("failed to find object %s : %s", id.Kind, id.Name)
	}
}
