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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/util/retry"
	"kpt.dev/configsync/e2e/nomostest"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	initialFirstCPU                    = 10
	initialFirstMemory                 = 100
	initialTotalCPU                    = 80
	initialAdjustedTotalCPU            = 250
	initialTotalMemory                 = 600
	autopilotCPUIncrements             = 250
	memoryMB                           = 1048576
	expectedFirstContainerCPU1         = 180
	expectedFirstContainerCPU2         = 430
	expectedFirstContainerMemory1      = 100
	expectedFirstContainerMemory2      = 200
	updatedFirstContainerCPULimit      = "500m"
	updatedFirstContainerMemoryLimit   = "500Mi"
	expectedFirstContainerMemoryLimit1 = "100Mi"
	expectedFirstContainerCPULimit1    = "180m"
)

func TestManagingReconciler(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	reconcilerDeployment := &appsv1.Deployment{}
	if err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, reconcilerDeployment); err != nil {
		nt.T.Fatal(err)
	}
	generation := reconcilerDeployment.Generation
	managedImage := reconcilerDeployment.Spec.Template.Spec.Containers[0].Image
	managedReplicas := *reconcilerDeployment.Spec.Replicas
	originalTolerations := reconcilerDeployment.Spec.Template.Spec.Tolerations

	// test case 1: The reconciler-manager should manage most of the fields with one exception:
	// - changes to the container resource requirements should be ignored when the autopilot annotation is set.
	nt.T.Log("Manually update the container image")
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		newImage := managedImage + "-updated"
		d.Spec.Template.Spec.Containers[0].Image = newImage
	})
	nt.T.Log("Verify the container image should be reverted by the reconciler-manager")
	generation += 2 // generation bumped by 2 because the change will be first applied then reverted by the reconciler-manager
	nomostest.Wait(nt.T, "the container image to be reverted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			firstContainerImageIs(managedImage))
	})

	// test case 2: the reconciler-manager should manage the replicas field, so that the reconciler can be resumed after pause.
	nt.T.Log("Manually update the replicas")
	newReplicas := managedReplicas - 1
	nt.MustMergePatch(reconcilerDeployment, fmt.Sprintf(`{"spec": {"replicas": %d}}`, newReplicas))
	nt.T.Log("Verify the reconciler-manager should revert the replicas change")
	generation += 2 // generation bumped by 2 because the change will be first applied then reverted by the reconciler-manager
	nomostest.Wait(nt.T, "the replicas to be reverted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			hasReplicas(managedReplicas))
	})

	// test case 3:  the reconciler-manager should not revert the change to the fields that are not owned by reconciler-manager
	nt.T.Log("Manually update fields that are not owned by reconciler-manager")
	var modifiedTolerations []corev1.Toleration
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].TerminationMessagePath = "dev/termination-message"
		d.Spec.Template.Spec.Containers[0].Stdin = true
		d.Spec.Template.Spec.Tolerations = append(d.Spec.Template.Spec.Tolerations, corev1.Toleration{Key: "kubernetes.io/arch", Effect: "NoSchedule", Operator: "Exists"})
		modifiedTolerations = d.Spec.Template.Spec.Tolerations
		d.Spec.Template.Spec.PriorityClassName = "system-node-critical"
	})

	nt.T.Log("Verify the reconciler-manager does not revert the change")
	generation++ // generation bumped by 1 because reconicler-manager should not revert this change
	nomostest.Wait(nt.T, "the reconciler deployment to be updated", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation), firstContainerTerminationMessagePathIs("dev/termination-message"),
			firstContainerStdinIs(true), hasTolerations(modifiedTolerations), hasPriorityClassName("system-node-critical"))
	})
	// change the fields back to default values
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].TerminationMessagePath = "dev/termination-log"
		d.Spec.Template.Spec.Containers[0].Stdin = false
		d.Spec.Template.Spec.Tolerations = originalTolerations
		d.Spec.Template.Spec.PriorityClassName = ""
	})
	generation++ // generation bumped by 1 because reconciler-manager should not revert this change
	nomostest.Wait(nt.T, "the reconciler deployment to be updated", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation), firstContainerTerminationMessagePathIs("dev/termination-log"),
			firstContainerStdinIs(false), hasTolerations(originalTolerations), hasPriorityClassName(""))
	})

	// test case 4: the reconciler-manager should update the reconciler Deployment if the manifest in the ConfigMap has been changed.
	nt.T.Log("Update the Deployment manifest in the ConfigMap")
	nt.MustKubectl("apply", "-f", "../testdata/reconciler-manager-configmap-updated.yaml")
	nt.T.Log("Restart the reconciler-manager to pick up the manifests change")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, true)
	// Reset the reconciler-manager in the cleanup stage so other test cases can still run in a shared testing cluster.
	nt.T.Cleanup(func() {
		generation++
		resetReconcilerDeploymentManifests(nt, managedImage, generation)
	})
	nt.T.Log("Verify the reconciler Deployment has been updated to the new manifest")
	generation++ // generation bumped by 1 to apply the new change in the default manifests declared in the Config Map
	nomostest.Wait(nt.T, "the deployment manifest to be updated", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			firstContainerImageIsNot(managedImage))
	})

	// test case 5: the reconciler-manager should delete the git-creds volume if not needed
	currentVolumesCount := len(reconcilerDeployment.Spec.Template.Spec.Volumes)
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Switch the auth type from ssh to none")
	nt.MustMergePatch(rs, `{"spec": {"git": {"auth": "none", "secretRef": {"name":""}}}}`)
	nt.T.Log("Verify the git-creds volume is gone")
	generation++ // generation bumped by 1 to delete the git-creds volume
	nomostest.Wait(nt.T, "the git-creds volume to be deleted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			gitCredsVolumeDeleted(currentVolumesCount))
	})

	// test case 6: the reconciler-manager should add the gcenode-askpass-sidecar container when needed
	nt.T.Log("Switch the auth type from none to gcpserviceaccount")
	nt.MustMergePatch(rs, `{"spec":{"git":{"auth":"gcpserviceaccount","secretRef":{"name":""},"gcpServiceAccountEmail":"test-gcp-sa-email@test-project.iam.gserviceaccount.com"}}}`)
	nt.T.Log("Verify the gcenode-askpass-sidecar container should be added")
	if nt.IsGKEAutopilot {
		generation += 2 // generation bumped by 2 because the sidecar container will first be added and then the resource requirements will be adjusted by autopilot
	} else {
		generation++ // generation bumped by 1 to apply the new sidecar container
	}
	nomostest.Wait(nt.T, "the gcenode-askpass-sidecar container to be added", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			templateForGcpServiceAccountAuthType())
	})

	// test case 7: the reconciler-manager should mount the git-creds volumes again if the auth type requires a git secret
	nt.T.Log("Switch the auth type gcpserviceaccount to ssh")
	nt.MustMergePatch(rs, `{"spec":{"git":{"auth":"ssh","secretRef":{"name":"git-creds"}}}}`)
	nt.T.Log("Verify the git-creds volume exists and the gcenode-askpass-sidecar container is gone")
	generation++ // generation bumped by 1 to add the git-cred volume again
	nomostest.Wait(nt.T, "the git-creds volume to be added and the gcenode-askpass-sidecar container to be deleted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			templateForSSHAuthType())
	})
}

type updateFunc func(deployment *appsv1.Deployment)

func mustUpdateRootReconciler(nt *nomostest.NT, f updateFunc) {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		d := &appsv1.Deployment{}
		if err := nt.KubeClient.Get(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, d); err != nil {
			return err
		}
		f(d)
		return nt.KubeClient.Update(d)
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func resetReconcilerDeploymentManifests(nt *nomostest.NT, origImg string, generation int64) {
	nt.T.Log("Reset the Deployment manifest in the ConfigMap")
	if err := nomostest.ResetReconcilerManagerConfigMap(nt); err != nil {
		nt.T.Fatalf("failed to reset configmap: %v", err)
	}

	nt.T.Log("Restart the reconciler-manager to pick up the manifests change")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, true)

	nt.T.Log("Verify the reconciler Deployment has been reverted to the original manifest")
	nomostest.Wait(nt.T, "the deployment manifest to be reverted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			firstContainerImageIs(origImg))
	})
}

func hasGeneration(generation int64) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Generation != generation {
			return fmt.Errorf("expected generation: %d, got: %d", generation, d.Generation)
		}
		return nil
	}
}
func firstContainerTerminationMessagePathIs(terminationMessagePath string) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].TerminationMessagePath != terminationMessagePath {
			return fmt.Errorf("expected first container terminationMessagePath is: %s, got: %s", terminationMessagePath, d.Spec.Template.Spec.Containers[0].TerminationMessagePath)
		}
		return nil
	}
}
func firstContainerStdinIs(stdin bool) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Stdin != stdin {
			return fmt.Errorf("expected first container stdin is: %t, got: %t", stdin, d.Spec.Template.Spec.Containers[0].Stdin)
		}
		return nil
	}
}
func hasTolerations(tolerations []corev1.Toleration) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		for i, toleration := range d.Spec.Template.Spec.Tolerations {
			if !equality.Semantic.DeepEqual(toleration, tolerations[i]) {
				return fmt.Errorf("expected toleration is: %s, got: %s", tolerations[i].String(), toleration.String())
			}
		}
		return nil
	}
}
func hasPriorityClassName(priorityClassName string) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.PriorityClassName != priorityClassName {
			return fmt.Errorf("expected priorityClassName is: %s, got: %s", priorityClassName, d.Spec.Template.Spec.PriorityClassName)
		}
		return nil
	}
}
func firstContainerImageIs(image string) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Image != image {
			return fmt.Errorf("expected first image: %s, got: %s", image, d.Spec.Template.Spec.Containers[0].Image)
		}
		return nil
	}
}

func firstContainerImageIsNot(image string) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Image == image {
			return fmt.Errorf("expected first image not to be: %s, got: %s", image, d.Spec.Template.Spec.Containers[0].Image)
		}
		return nil
	}
}

func hasReplicas(replicas int32) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if *d.Spec.Replicas != replicas {
			return fmt.Errorf("expected replicas: %d, got: %d", replicas, *d.Spec.Replicas)
		}
		return nil
	}
}

func firstContainerMemoryLimitIs(memoryLimit string) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String() != memoryLimit {
			return fmt.Errorf("expected memory limit of the first container: %s, got: %s",
				memoryLimit, d.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().String())
		}
		return nil
	}
}

func firstContainerCPULimitIs(cpuLimit string) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String() != cpuLimit {
			return fmt.Errorf("expected CPU limit of the first container: %s, got: %s",
				cpuLimit, d.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu().String())
		}
		return nil
	}
}

func gitCredsVolumeDeleted(volumesCount int) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if len(d.Spec.Template.Spec.Volumes) != volumesCount-1 {
			return fmt.Errorf("expected volumes count: %d, got: %d",
				volumesCount-1, len(d.Spec.Template.Spec.Volumes))
		}
		for _, volume := range d.Spec.Template.Spec.Volumes {
			if volume.Name == controllers.GitCredentialVolume {
				return fmt.Errorf("the git-creds volume should be gone for `none` auth type")
			}
		}
		return nil
	}
}

func templateForGcpServiceAccountAuthType() nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		for _, volume := range d.Spec.Template.Spec.Volumes {
			if volume.Name == controllers.GitCredentialVolume {
				return fmt.Errorf("the git-creds volume should not exist for `gcpserviceaccount` auth type")
			}
		}
		for _, container := range d.Spec.Template.Spec.Containers {
			if container.Name == controllers.GceNodeAskpassSidecarName {
				return nil
			}
		}
		return fmt.Errorf("the %s container has not be created yet", controllers.GceNodeAskpassSidecarName)
	}
}

func templateForSSHAuthType() nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		for _, container := range d.Spec.Template.Spec.Containers {
			if container.Name == controllers.GceNodeAskpassSidecarName {
				return fmt.Errorf("the gcenode-askpass-sidecar container should not exist for `ssh` auth type")
			}
		}
		for _, volume := range d.Spec.Template.Spec.Volumes {
			if volume.Name == controllers.GitCredentialVolume {
				return nil
			}
		}
		return fmt.Errorf("the git-creds volume has not be created yet")
	}
}

func totalContainerMemoryRequestIs(memoryRequest int64) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		memoryTotal := getTotalContainerMemoryRequest(d)

		if int64(memoryTotal) != (memoryRequest * memoryMB) {
			return fmt.Errorf("expected total memory request of all containers: %d, got: %d",
				memoryRequest, memoryTotal)
		}
		return nil
	}
}

func getTotalContainerMemoryRequest(d *appsv1.Deployment) int {
	memoryTotal := 0

	for _, container := range d.Spec.Template.Spec.Containers {
		memoryTotal += int(container.Resources.Requests.Memory().Value())
	}

	return memoryTotal
}

func totalContainerCPURequestIs(expectedCPURequest int64) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		actualCPUTotal := getTotalContainerCPURequest(d)

		if int64(actualCPUTotal) != expectedCPURequest {
			return fmt.Errorf("expected total CPU request of all containers: %d, got: %d",
				expectedCPURequest, actualCPUTotal)
		}
		return nil
	}
}

func getTotalContainerCPURequest(d *appsv1.Deployment) int {
	cpuTotal := 0

	for _, container := range d.Spec.Template.Spec.Containers {
		cpuTotal += int(container.Resources.Requests.Cpu().MilliValue())
	}

	return cpuTotal
}

func firstContainerCPURequestIs(cpuRequest int64) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue() != cpuRequest {
			return fmt.Errorf("expected CPU request of the first container: %d, got: %d",
				cpuRequest, d.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue())
		}
		return nil
	}
}

func firstContainerMemoryRequestIs(memoryRequest int64) nomostest.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return nomostest.ErrObjectNotFound
		}
		memoryRequest *= memoryMB
		d, ok := o.(*appsv1.Deployment)
		if !ok {
			return nomostest.WrongTypeErr(d, &appsv1.Deployment{})
		}
		if d.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Value() != memoryRequest {
			return fmt.Errorf("expected memory request of the first container: %d, got: %d",
				memoryRequest, d.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().MilliValue())
		}
		return nil
	}
}

func TestAutopilotReconcilerAdjustment(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	reconcilerDeployment := &appsv1.Deployment{}
	if err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, reconcilerDeployment); err != nil {
		nt.T.Fatal(err)
	}
	firstContainerName := reconcilerDeployment.Spec.Template.Spec.Containers[0].Name

	rs := &v1beta1.RootSync{}

	if err := nt.KubeClient.Get(configsync.RootSyncName, configsync.ControllerNamespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	generation := reconcilerDeployment.Generation
	var expectedTotalCPU int64
	var expectedTotalMemory int64
	var expectedFirstContainerCPU int64
	var expectedFirstContainerMemory int64
	var expectedFirstContainerMemoryLimit string
	var expectedFirstContainerCPULimit string
	firstContainerCPURequest := reconcilerDeployment.Spec.Template.Spec.Containers[0].Resources.Requests.Cpu().MilliValue()
	firstContainerMemoryRequest := reconcilerDeployment.Spec.Template.Spec.Containers[0].Resources.Requests.Memory().Value() / memoryMB
	input := map[string]corev1.ResourceRequirements{}
	output := map[string]corev1.ResourceRequirements{}

	// default container resource requests defined in the reconciler template: manifests/templates/reconciler-manager-configmap.yaml, total CPU/memory: 80m/600Mi
	// - hydration-controller: 10m/100Mi
	// - reconciler: 50m/200Mi
	// - git-sync: 10m/200Mi
	// - otel-agent: 10m/100Mi
	// initial generation = 1, total memory 629145600 (600Mi)

	if nt.IsGKEAutopilot {
		// with autopilot adjustment, CPU of container[0] is increased to 180m
		// bringing the total to 250m
		expectedTotalCPU = initialAdjustedTotalCPU
		expectedFirstContainerCPU = expectedFirstContainerCPU1
	} else {
		expectedTotalCPU = initialTotalCPU
		expectedFirstContainerCPU = initialFirstCPU
	}
	expectedTotalMemory = initialTotalMemory

	nt.T.Log("Validating initial container request value")
	if err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
		hasGeneration(generation), totalContainerCPURequestIs(expectedTotalCPU),
		totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU)); err != nil {
		nt.T.Error(err)
	}

	// increase CPU and memory request to above current request autopilot increment
	nt.T.Log("Increasing CPU and memory request to above current request autopilot increment")
	updatedFirstContainerCPURequest := firstContainerCPURequest + 10
	updatedFirstContainerMemoryRequest := firstContainerMemoryRequest + 100
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryRequest":"%dMi", "cpuRequest":"%dm"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryRequest, updatedFirstContainerCPURequest))
	// generation = 2
	generation++

	if nt.IsGKEAutopilot {
		// hydration-controller input: 190m/200Mi
		// with autopilot adjustment, CPU of container[0]/hydration-controller is increased to 430m
		// bringing the total to 500m
		expectedTotalCPU += autopilotCPUIncrements
		expectedFirstContainerCPU = expectedFirstContainerCPU2
		expectedFirstContainerMemory = expectedFirstContainerMemory2
	} else {
		expectedTotalCPU = initialTotalCPU + 10
		expectedFirstContainerCPU = updatedFirstContainerCPURequest
		expectedFirstContainerMemory = updatedFirstContainerMemoryRequest
	}
	expectedTotalMemory += 100
	// Waiting for reconciliation and validation
	nomostest.Wait(nt.T, "the first container resource requests to be increased", nt.DefaultWaitTimeout, func() error {
		rd := &appsv1.Deployment{}
		err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, rd,
			hasGeneration(generation), totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory))
		if err != nil {
			return err
		}
		if nt.IsGKEAutopilot {
			input, output, err = util.AutopilotResourceMutation(rd.Annotations[metadata.AutoPilotAnnotation])
			if err != nil {
				return err
			}
		}
		return nil
	})

	// manually increase first container CPU and memory request to above current user override request
	nt.T.Log("Manually update the first container CPU and Memory request")
	manualFirstContainerCPURequest := updatedFirstContainerCPURequest + 10
	manualFirstContainerMemoryRequest := updatedFirstContainerMemoryRequest + 10

	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			"cpu":    resource.MustParse(fmt.Sprintf("%dm", manualFirstContainerCPURequest)),
			"memory": resource.MustParse(fmt.Sprintf("%dMi", manualFirstContainerMemoryRequest)),
		}
	})
	nt.T.Log("Verify the reconciler-manager does revert the manual memory/CPU request change")
	generation += 2
	nomostest.Wait(nt.T, "the resource requests update to be reverted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			hasGeneration(generation), totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory))
	})
	// decrease CPU to below autopilot increment but above current request
	nt.T.Log("Decreasing CPU request to above current request but below autopilot increment")
	if nt.IsGKEAutopilot {
		inputRequest := input[firstContainerName].Requests
		inputCPU := inputRequest.Cpu().MilliValue()
		outputRequest := output[firstContainerName].Requests
		outputCPU := outputRequest.Cpu().MilliValue()
		updatedFirstContainerCPURequest = (inputCPU + outputCPU) / 2
		// autopilot will not adjust CPU and generation
		expectedFirstContainerCPU = expectedFirstContainerCPU2
		expectedFirstContainerMemory = expectedFirstContainerMemory2
	} else {
		// since standard cluster doesnt have resource adjustment annotation
		// validation data are assigned separately than autopilot cluster
		updatedFirstContainerCPURequest -= 10
		expectedTotalCPU -= 10
		expectedFirstContainerCPU = updatedFirstContainerCPURequest
		expectedFirstContainerMemory = updatedFirstContainerMemoryRequest
		generation++
	}
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryRequest":"%dMi", "cpuRequest":"%dm"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryRequest, updatedFirstContainerCPURequest))

	nomostest.Wait(nt.T, "resource CPU request to be updated only on non-autopilot cluster", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			hasGeneration(generation), totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory))
	})

	// Manually decrease cpu request to below current cpu request, but the output cpu request after autopilot adjustment are greater than current cpu request
	nt.T.Log("Manually decrease the first container CPU request")
	manualFirstContainerCPURequest = updatedFirstContainerCPURequest - 10
	if nt.IsGKEAutopilot {
		generation++
	} else {
		generation += 2
	}
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			"cpu": resource.MustParse(fmt.Sprintf("%dm", manualFirstContainerCPURequest)),
		}
	})
	nomostest.Wait(nt.T, "resource CPU request manual update to be reverted only on non-autopilot cluster", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			hasGeneration(generation), totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory))
	})
	// Manually decrease cpu and memory requests to below current requests, and the output resource requests after autopilot adjustment still below current requests
	nt.T.Log("Manually decrease the first container CPU and Memory request")
	manualFirstContainerCPURequest = initialFirstCPU
	manualFirstContainerMemoryRequest = initialFirstMemory
	generation += 2
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Requests = corev1.ResourceList{
			"cpu":    resource.MustParse(fmt.Sprintf("%dm", manualFirstContainerCPURequest)),
			"memory": resource.MustParse(fmt.Sprintf("%dMi", manualFirstContainerMemoryRequest)),
		}
	})
	nomostest.Wait(nt.T, "the resource requests manual update to be reverted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			hasGeneration(generation), totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory))
	})
	// decrease cpu and memory request to below current request autopilot increment
	nt.T.Log("Reverting CPU and memory back to initial value")
	updatedFirstContainerCPURequest = initialFirstCPU
	updatedFirstContainerMemoryRequest = initialFirstMemory
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryRequest":"%dMi", "cpuRequest":"%dm"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryRequest, updatedFirstContainerCPURequest))
	// increment generation for both standard and autopilot
	generation++
	if nt.IsGKEAutopilot {
		// hydration-controller input: 10m/100Mi
		// with autopilot adjustment, first container is adjusted to 180m
		// bringing the total CPU request to 250
		expectedTotalCPU -= autopilotCPUIncrements
		expectedTotalMemory = initialTotalMemory
		expectedFirstContainerCPU = expectedFirstContainerCPU1
		expectedFirstContainerMemory = expectedFirstContainerMemory1
	} else {
		expectedFirstContainerCPU = updatedFirstContainerCPURequest
		expectedFirstContainerMemory = updatedFirstContainerMemoryRequest
		expectedTotalCPU = initialTotalCPU
		expectedTotalMemory = initialTotalMemory
	}

	nomostest.Wait(nt.T, "resource requests to be updated", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			hasGeneration(generation), totalContainerCPURequestIs(expectedTotalCPU),
			totalContainerMemoryRequestIs(expectedTotalMemory), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory))
	})

	// limit change
	// The reconciler-manager allows manual change to resource limits on non-Autopilot clusters only if the resource limits are neither defined by us in reonciler-manager-configmap
	// nor users through spec.override.resources field.
	// All the changes to resource limits on Autopilot cluster will be ignored, since the resource limits are always same with resource requests on Autopilot cluster

	// Manually update resource limits before override resource limits through API
	nt.T.Log("Manually update the first container CPU and Memory limit")
	manualFirstContainerCPULimit := "600m"
	manualFirstContainerMemoryLimit := "600Mi"
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			"cpu":    resource.MustParse(manualFirstContainerCPULimit),
			"memory": resource.MustParse(manualFirstContainerMemoryLimit),
		}
	})
	nt.T.Log("Verify the reconciler-manager does not revert the manual resource limits change when user does not override the memory limit ")
	if nt.IsGKEAutopilot {
		expectedFirstContainerMemoryLimit = expectedFirstContainerMemoryLimit1
		expectedFirstContainerCPULimit = expectedFirstContainerCPULimit1
	} else {
		expectedFirstContainerMemoryLimit = manualFirstContainerMemoryLimit
		expectedFirstContainerCPULimit = manualFirstContainerCPULimit
	}

	generation++ // generation bumped by 1 because the memory limits are still not owned by reconciler manager
	nomostest.Wait(nt.T, "resource limits to be updated on non-autopilot cluster, ignore this update on autopilot cluster", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			firstContainerMemoryLimitIs(expectedFirstContainerMemoryLimit), firstContainerCPULimitIs(expectedFirstContainerCPULimit))
	})

	// override the resource limits through spec.override.resources field
	nt.T.Log("Updating the container limits")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryLimit":"%s", "cpuLimit":"%s"}]}}}`,
		firstContainerName, updatedFirstContainerMemoryLimit, updatedFirstContainerCPULimit))

	if nt.IsGKEAutopilot {
		// overriding the limit should not trigger any changes
		expectedFirstContainerCPU = expectedFirstContainerCPU1
		expectedFirstContainerMemory = expectedFirstContainerMemory1
		expectedFirstContainerMemoryLimit = expectedFirstContainerMemoryLimit1
		expectedFirstContainerCPULimit = expectedFirstContainerCPULimit1

	} else {
		expectedFirstContainerMemoryLimit = updatedFirstContainerMemoryLimit
		expectedFirstContainerCPULimit = updatedFirstContainerCPULimit
		generation++
	}
	nomostest.Wait(nt.T, "resource limits to be updated on non-autopilot cluster, ignore this update on autopilot cluster", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{},
			hasGeneration(generation), firstContainerCPURequestIs(expectedFirstContainerCPU),
			firstContainerMemoryRequestIs(expectedFirstContainerMemory), firstContainerMemoryLimitIs(expectedFirstContainerMemoryLimit),
			firstContainerCPULimitIs(expectedFirstContainerCPULimit))
	})

	//manually update the resource limits after user override the resource limits through spec.override field, in this case, reconciler-manager own the resource limits field
	nt.T.Log("Manually update the first container CPU and Memory limit")
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		d.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
			"cpu":    resource.MustParse(manualFirstContainerCPULimit),
			"memory": resource.MustParse(manualFirstContainerMemoryLimit),
		}
	})
	nt.T.Log("Verify the reconciler-manager does revert the manual resource limits change after user override the resource limits through API")
	if nt.IsGKEAutopilot {
		generation++
	} else {
		generation += 2
	}
	nomostest.Wait(nt.T, "the manual resource limits change to be reverted on non-autopilot cluster, ignore this change on autopilot cluster", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			firstContainerMemoryLimitIs(expectedFirstContainerMemoryLimit),
			firstContainerCPULimitIs(expectedFirstContainerCPULimit))
	})
}
