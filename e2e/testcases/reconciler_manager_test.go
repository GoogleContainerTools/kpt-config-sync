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
	"path/filepath"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/util/retry"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestManagingReconciler(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo)

	reconcilerDeployment := &appsv1.Deployment{}
	if err := nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, reconcilerDeployment); err != nil {
		nt.T.Fatal(err)
	}
	generation := reconcilerDeployment.Generation
	managedImage := reconcilerDeployment.Spec.Template.Spec.Containers[0].Image
	managedReplicas := *reconcilerDeployment.Spec.Replicas
	firstContainerName := reconcilerDeployment.Spec.Template.Spec.Containers[0].Name
	managedMemoryLimit := reconcilerDeployment.Spec.Template.Spec.Containers[0].Resources.Limits.Memory().DeepCopy()

	// test case 1: The reconciler-manager should manage most of the fields with one exception:
	// - changes to the container resource requirements should be ignored when the autopilot annotation is set.
	nt.T.Log("Manually update the container image")
	mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
		newImage := managedImage + "-updated"
		d.Spec.Template.Spec.Containers[0].Image = newImage
	})
	nt.T.Log("Verify the container image should be reverted by the reconciler-manager")
	generation += 2
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
	generation += 2
	nomostest.Wait(nt.T, "the replicas to be reverted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			hasReplicas(managedReplicas))
	})

	// test case 3: override the memory limit
	rs := &v1beta1.RootSync{}
	updatedContainerMemoryLimit := &resource.Quantity{}
	nt.T.Log("Override the memory limit with new value")
	managedMemoryLimit.DeepCopyInto(updatedContainerMemoryLimit)
	updatedContainerMemoryLimit.Add(resource.MustParse("5Mi"))
	if err := nt.Get(configsync.RootSyncName, configsync.ControllerNamespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec":{"override":{"resources":[{"containerName":"%s","memoryLimit":"%s"}]}}}`,
		firstContainerName, updatedContainerMemoryLimit.String()))
	if nt.IsGKEAutopilot {
		// test case 3.a: the reconciler-manager should not override the container resource requirements on autopilot clusters.
		nt.T.Log("Verify the reconciler-manager should ignore the memory limit override")
		nomostest.Wait(nt.T, "the memory limit override to be ignored", nt.DefaultWaitTimeout, func() error {
			return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
				&appsv1.Deployment{}, hasGeneration(generation), // generation remains the same because of no change.
				firstContainerMemoryLimitIs(managedMemoryLimit.String()))
		})
	} else {
		// test case 3.b: the reconciler-manager should support overriding the container resource requirements on non-autopilot clusters.
		nt.T.Log("Verify the reconciler-manager should override the memory limit")
		generation++
		nomostest.Wait(nt.T, "the memory limit to be overridden", nt.DefaultWaitTimeout, func() error {
			return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
				&appsv1.Deployment{}, hasGeneration(generation),
				firstContainerMemoryLimitIs(updatedContainerMemoryLimit.String()))
		})
	}

	// test case 4: manually update the memory limit
	if nt.IsGKEAutopilot {
		// test case 4.a: the reconciler-manager should not manage the container resource requirements on autopilot clusters.
		nt.T.Log("Manually update the memory limit")
		mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				"cpu":    *d.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu(),
				"memory": *updatedContainerMemoryLimit,
			}
		})
		nt.T.Log("Verify the reconciler-manager should not revert the change")
		generation++
		// We can't verify the memory limit because Autopilot controls the limit and it may not be updated.
		nomostest.Wait(nt.T, "the memory limit to be updated by the autopilot", nt.DefaultWaitTimeout, func() error {
			return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
				&appsv1.Deployment{}, hasGeneration(generation))
		})
	} else {
		// test case 4.b: the reconciler-manager should not allow updating the container resource requirements on non-autopilot clusters.
		nt.T.Log("Manually set the memory limit back to the original value")
		mustUpdateRootReconciler(nt, func(d *appsv1.Deployment) {
			d.Spec.Template.Spec.Containers[0].Resources.Limits = corev1.ResourceList{
				"cpu":    *d.Spec.Template.Spec.Containers[0].Resources.Limits.Cpu(),
				"memory": managedMemoryLimit,
			}
		})
		nt.T.Log("Verify the reconciler-manager should revert the memory limit change")
		generation += 2
		nomostest.Wait(nt.T, "the memory limit to be reverted", nt.DefaultWaitTimeout, func() error {
			return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
				&appsv1.Deployment{}, hasGeneration(generation),
				firstContainerMemoryLimitIs(updatedContainerMemoryLimit.String()))
		})
	}

	// test case 5: the reconciler-manager should update the reconciler Deployment if the manifest in the ConfigMap has been changed.
	nt.T.Log("Update the Deployment manifest in the ConfigMap")
	nt.MustKubectl("apply", "-f", "../testdata/reconciler-manager-configmap-updated.yaml")
	nt.T.Log("Restart the reconciler-manager to pick up the manifests change")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName)
	// Reset the reconciler-manager in the cleanup stage so other test cases can still run in a shared testing cluster.
	nt.T.Cleanup(func() {
		generation++
		resetReconcilerDeploymentManifests(nt, managedImage, generation)
	})
	nt.T.Log("Verify the reconciler Deployment has been updated to the new manifest")
	generation++
	nomostest.Wait(nt.T, "the deployment manifest to be updated", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			firstContainerImageIsNot(managedImage))
	})

	// test case 6: the reconciler-manager should delete the git-creds volume if not needed
	currentVolumesCount := len(reconcilerDeployment.Spec.Template.Spec.Volumes)
	nt.T.Log("Switch the auth type from ssh to none")
	nt.MustMergePatch(rs, `{"spec": {"git": {"auth": "none", "secretRef": {"name":""}}}}`)
	nt.T.Log("Verify the git-creds volume is gone")
	generation++
	nomostest.Wait(nt.T, "the git-creds volume to be deleted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			gitCredsVolumeDeleted(currentVolumesCount))
	})

	// test case 7: the reconciler-manager should add the gcenode-askpass-sidecar container when needed
	nt.T.Log("Switch the auth type from none to gcpserviceaccount")
	nt.MustMergePatch(rs, `{"spec":{"git":{"auth":"gcpserviceaccount","secretRef":{"name":""},"gcpServiceAccountEmail":"test-gcp-sa-email@test-project.iam.gserviceaccount.com"}}}`)
	nt.T.Log("Verify the gcenode-askpass-sidecar container should be added")
	generation++
	nomostest.Wait(nt.T, "the gcenode-askpass-sidecar container to be added", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			templateForGcpServiceAccountAuthType())
	})

	// test case 8: the reconciler-manager should mount the git-creds volumes again if the auth type requires a git secret
	nt.T.Log("Switch the auth type gcpserviceaccount to ssh")
	nt.MustMergePatch(rs, `{"spec":{"git":{"auth":"ssh","secretRef":{"name":"git-creds"}}}}`)
	nt.T.Log("Verify the git-creds volume exists and the gcenode-askpass-sidecar container is gone")
	generation++
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
		if err := nt.Get(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, d); err != nil {
			return err
		}
		f(d)
		return nt.Update(d)
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func resetReconcilerDeploymentManifests(nt *nomostest.NT, origImg string, generation int64) {
	nt.T.Log("Reset the Deployment manifest in the ConfigMap")
	var originalManifestFile string
	if *e2e.ShareTestEnv {
		originalManifestFile = filepath.Join(nomostest.SharedNT().TmpDir, nomostest.Manifests, "reconciler-manager-configmap.yaml")
	} else {
		originalManifestFile = filepath.Join(nt.TmpDir, nomostest.Manifests, "reconciler-manager-configmap.yaml")
	}
	nt.MustKubectl("apply", "-f", originalManifestFile)

	nt.T.Log("Restart the reconciler-manager to pick up the manifests change")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName)

	nt.T.Log("Verify the reconciler Deployment has been reverted to the original manifest")
	nomostest.Wait(nt.T, "the deployment manifest to be reverted", nt.DefaultWaitTimeout, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem,
			&appsv1.Deployment{}, hasGeneration(generation),
			firstContainerImageIs(origImg))
	})
}

func hasGeneration(generation int64) nomostest.Predicate {
	return func(o client.Object) error {
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

func firstContainerImageIs(image string) nomostest.Predicate {
	return func(o client.Object) error {
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

func gitCredsVolumeDeleted(volumesCount int) nomostest.Predicate {
	return func(o client.Object) error {
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
