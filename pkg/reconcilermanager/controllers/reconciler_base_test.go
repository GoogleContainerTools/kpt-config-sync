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

package controllers

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAdjustContainerResources(t *testing.T) {
	// containerResources1 is compliant with Autopilot constraints
	containerResources1 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("412Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("412Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}

	// containerResources2 is compliant with Autopilot constraints
	containerResources2 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}

	// containerResources3 is NOT compliant with Autopilot constraints
	containerResources3 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("50Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	// containerResources4 is NOT compliant with Autopilot constraints
	containerResources4 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("200Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	// containerResources5 is NOT compliant with Autopilot constraints
	containerResources5 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("800m"),
				corev1.ResourceMemory: resource.MustParse("768Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	// containerResources6 is compliant with Autopilot constraints
	containerResources6 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("740m"),
				corev1.ResourceMemory: resource.MustParse("668Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("740m"),
				corev1.ResourceMemory: resource.MustParse("668Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
		},
	}

	// containerResources7 is NOT compliant with Autopilot constraints
	containerResources7 := map[string]corev1.ResourceRequirements{
		reconcilermanager.Reconciler: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("240m"),
				corev1.ResourceMemory: resource.MustParse("312Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("300m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
		reconcilermanager.GitSync: {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("10m"),
				corev1.ResourceMemory: resource.MustParse("100Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("500Mi"),
			},
		},
	}

	adjust3To1, err := adjustmentAnnotation(containerResources3, containerResources1)
	if err != nil {
		t.Error(err)
	}
	adjust5To6, err := adjustmentAnnotation(containerResources5, containerResources6)
	if err != nil {
		t.Error(err)
	}

	testCases := []struct {
		name                       string
		vpaEnabled                 bool
		isAutopilot                bool
		declaredContainerResources map[string]corev1.ResourceRequirements
		currentContainerResources  map[string]corev1.ResourceRequirements
		currentAnnotations         map[string]string
		adjusted                   bool
		expectedDeclaredDeployment *appsv1.Deployment
	}{
		{
			name:                       "No VPA, No Autopilot, declared is same as current => declared not adjusted, current won't be updated",
			vpaEnabled:                 false,
			isAutopilot:                false,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources1,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		{
			name:                       "No VPA, No Autopilot, declared is different from current => declared not adjusted, current will be updated by the controller",
			vpaEnabled:                 false,
			isAutopilot:                false,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources2,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		{
			name:                       "VPA is enabled, declared is same as current => declared not adjusted, current won't be updated",
			vpaEnabled:                 true,
			isAutopilot:                false,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources1,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		{
			name:                       "VPA is enabled, declared is different from current => declared adjusted to current to avoid discrepancy, current won't be updated",
			vpaEnabled:                 true,
			isAutopilot:                false,
			adjusted:                   true,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources2,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources2)),
		},
		{
			name:                       "No VPA, Autopilot enabled, not adjusted by Autopilot yet, declared is same as current => declared not adjusted, current won't be updated",
			vpaEnabled:                 false,
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources1,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		{
			// The declared resources can be higher or lower than the current.
			// If the declared resources are compliant with Autopilot constraints, no update.
			// Otherwise, Autopilot will adjust the resources, which will trigger a reconciliation.
			name:                       "No VPA, Autopilot enabled, not adjusted by Autopilot yet, declared is different from current => declared not adjusted, current will be updated by the controller",
			vpaEnabled:                 false,
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources1,
			currentContainerResources:  containerResources2,
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		// Mimic the case of increasing the resources.
		{
			name:                       "No VPA, Autopilot enabled, adjusted by Autopilot, one or more declared resources are higher than adjusted => declared not adjusted, current will be updated by the controller",
			vpaEnabled:                 false,
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources2,
			currentContainerResources:  containerResources1,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: adjust3To1},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources2)),
		},
		// Autopilot further increases the resources to meet the constraints, so no more update is needed.
		{
			name:                       "No VPA, Autopilot enabled, adjusted by Autopilot, all declared resources are higher than or equal to input, but all are lower than or equal to adjusted => declared adjusted to current to avoid discrepancy, current won't be updated",
			vpaEnabled:                 false,
			isAutopilot:                true,
			adjusted:                   true,
			declaredContainerResources: containerResources4,
			currentContainerResources:  containerResources1,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: adjust3To1},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources1)),
		},
		// Mimic the case of decreasing the resource.
		{
			name:                       "No VPA, Autopilot enabled, adjusted by Autopilot, one or more declared resources are lower than input => declared not adjusted, current will be updated by the controller",
			vpaEnabled:                 false,
			isAutopilot:                true,
			adjusted:                   false,
			declaredContainerResources: containerResources7,
			currentContainerResources:  containerResources6,
			currentAnnotations:         map[string]string{metadata.AutoPilotAnnotation: adjust5To6},
			expectedDeclaredDeployment: fake.DeploymentObject(addContainerWithResources(containerResources7)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			declared := fake.DeploymentObject()
			current := fake.DeploymentObject()

			for containerName, resources := range tc.declaredContainerResources {
				declared.Spec.Template.Spec.Containers = append(declared.Spec.Template.Spec.Containers, corev1.Container{Name: containerName, Resources: resources})
			}

			for containerName, resources := range tc.currentContainerResources {
				current.Spec.Template.Spec.Containers = append(current.Spec.Template.Spec.Containers, corev1.Container{Name: containerName, Resources: resources})
			}

			current.Annotations = map[string]string{}
			for key, value := range tc.currentAnnotations {
				current.Annotations[key] = value
			}

			got, err := adjustContainerResources(tc.vpaEnabled, tc.isAutopilot, declared, current)
			if err != nil {
				t.Errorf("%s: got unexpected error: %v", tc.name, err)
			}
			if got != tc.adjusted {
				t.Errorf("%s: adjusted, got %t, want %t", tc.name, got, tc.adjusted)
			}
			if diff := cmp.Diff(declared.Spec.Template.Spec.Containers, tc.expectedDeclaredDeployment.Spec.Template.Spec.Containers, cmpopts.EquateEmpty(), cmpopts.SortSlices(
				func(x, y corev1.Container) bool { return x.Name < y.Name })); diff != "" {
				t.Errorf("%s: declared Deployment diff: %s", tc.name, diff)
			}
		})
	}
}

func addContainerWithResources(resources map[string]corev1.ResourceRequirements) core.MetaMutator {
	return func(obj client.Object) {
		o, ok := obj.(*appsv1.Deployment)
		if ok {
			for n, r := range resources {
				o.Spec.Template.Spec.Containers = append(o.Spec.Template.Spec.Containers, corev1.Container{Name: n, Resources: r})
			}
		}
	}
}

func adjustmentAnnotation(input, output map[string]corev1.ResourceRequirements) (string, error) {
	adjustment := util.ResourceMutation{
		Input:    mapToPodResources(input),
		Output:   mapToPodResources(output),
		Modified: true,
	}

	annotation, err := json.Marshal(adjustment)
	if err != nil {
		return "", fmt.Errorf("failed to marshal the adjusted resources: %w", err)
	}
	return string(annotation), nil
}

func mapToPodResources(m map[string]corev1.ResourceRequirements) *util.PodResources {
	var containers []util.ContainerResources
	for name, resources := range m {
		containers = append(containers, util.ContainerResources{
			Name:                 name,
			ResourceRequirements: resources,
		})
	}
	return &util.PodResources{Containers: containers}
}
