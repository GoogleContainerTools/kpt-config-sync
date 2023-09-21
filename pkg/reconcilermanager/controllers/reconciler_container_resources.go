// Copyright 2023 Google LLC
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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

// ReconcilerContainerResourceDefaults are the default resources to use for the
// reconciler deployment containers.
// These defaults should be high enough to work for most users our of the box,
// with a moderately high number of resource objects (e.g. 1k).
func ReconcilerContainerResourceDefaults() map[string]v1beta1.ContainerResourcesSpec {
	return map[string]v1beta1.ContainerResourcesSpec{
		reconcilermanager.Reconciler: {
			ContainerName: reconcilermanager.Reconciler,
			CPURequest:    resource.MustParse("50m"),
			MemoryRequest: resource.MustParse("200Mi"),
		},
		reconcilermanager.HydrationController: {
			ContainerName: reconcilermanager.HydrationController,
			CPURequest:    resource.MustParse("10m"),
			MemoryRequest: resource.MustParse("100Mi"),
		},
		reconcilermanager.OciSync: {
			ContainerName: reconcilermanager.OciSync,
			CPURequest:    resource.MustParse("10m"),
			MemoryRequest: resource.MustParse("200Mi"),
		},
		reconcilermanager.HelmSync: {
			ContainerName: reconcilermanager.HelmSync,
			CPURequest:    resource.MustParse("50m"),
			MemoryRequest: resource.MustParse("200Mi"),
		},
		reconcilermanager.GitSync: {
			ContainerName: reconcilermanager.GitSync,
			CPURequest:    resource.MustParse("10m"),
			MemoryRequest: resource.MustParse("200Mi"),
		},
		reconcilermanager.GCENodeAskpassSidecar: {
			ContainerName: reconcilermanager.GCENodeAskpassSidecar,
			CPURequest:    resource.MustParse("50m"),
			MemoryRequest: resource.MustParse("20Mi"),
		},
		metrics.OtelAgentName: {
			ContainerName: metrics.OtelAgentName,
			CPURequest:    resource.MustParse("10m"),
			CPULimit:      resource.MustParse("1000m"),
			MemoryRequest: resource.MustParse("100Mi"),
			MemoryLimit:   resource.MustParse("1Gi"),
		},
	}
}

// setContainerResourceDefaults sets the defaults when not specified in the
// overrides.
func setContainerResourceDefaults(overrides []v1beta1.ContainerResourcesSpec, defaultsMap map[string]v1beta1.ContainerResourcesSpec) []v1beta1.ContainerResourcesSpec {
	// Convert overrides list to map, indexed by container name
	overrideMap := make(map[string]v1beta1.ContainerResourcesSpec)
	for _, override := range overrides {
		overrideMap[override.ContainerName] = override
	}
	// Merge the defaults with the overrides.
	// Only use the default if the override is not specified.
	for containerName, defaults := range defaultsMap {
		override, found := overrideMap[containerName]
		if !found {
			// No overrides specified for this container - use the defaults as-is (copy struct)
			overrideMap[containerName] = v1beta1.ContainerResourcesSpec{
				CPURequest:    defaults.CPURequest,
				CPULimit:      defaults.CPULimit,
				MemoryRequest: defaults.MemoryRequest,
				MemoryLimit:   defaults.MemoryLimit,
			}
			continue
		}
		updated := v1beta1.ContainerResourcesSpec{}
		// Some overrides specified for this container - use default, if no override is specified for each value
		if !override.CPURequest.IsZero() {
			updated.CPURequest = override.CPURequest
		} else if !defaults.CPURequest.IsZero() {
			updated.CPURequest = defaults.CPURequest
		}
		if !override.CPULimit.IsZero() {
			updated.CPULimit = override.CPULimit
		} else if !defaults.CPULimit.IsZero() {
			updated.CPULimit = defaults.CPULimit
		}
		if !override.MemoryRequest.IsZero() {
			updated.MemoryRequest = override.MemoryRequest
		} else if !defaults.MemoryRequest.IsZero() {
			updated.MemoryRequest = defaults.MemoryRequest
		}
		if !override.MemoryLimit.IsZero() {
			updated.MemoryLimit = override.MemoryLimit
		} else if !defaults.MemoryLimit.IsZero() {
			updated.MemoryLimit = defaults.MemoryLimit
		}
		overrideMap[containerName] = updated
	}
	// Convert back to list
	overrides = make([]v1beta1.ContainerResourcesSpec, 0, len(overrideMap))
	for containerName, override := range overrideMap {
		override.ContainerName = containerName
		overrides = append(overrides, override)
	}
	return overrides
}

func mutateContainerResource(c *corev1.Container, overrides []v1beta1.ContainerResourcesSpec) {
	if len(overrides) == 0 {
		return
	}

	for _, override := range overrides {
		if override.ContainerName == c.Name {
			if !override.CPURequest.IsZero() {
				if c.Resources.Requests == nil {
					c.Resources.Requests = corev1.ResourceList{}
				}
				c.Resources.Requests[corev1.ResourceCPU] = override.CPURequest
			}
			if !override.CPULimit.IsZero() {
				if c.Resources.Limits == nil {
					c.Resources.Limits = corev1.ResourceList{}
				}
				c.Resources.Limits[corev1.ResourceCPU] = override.CPULimit
			}
			if !override.MemoryRequest.IsZero() {
				if c.Resources.Requests == nil {
					c.Resources.Requests = corev1.ResourceList{}
				}
				c.Resources.Requests[corev1.ResourceMemory] = override.MemoryRequest
			}
			if !override.MemoryLimit.IsZero() {
				if c.Resources.Limits == nil {
					c.Resources.Limits = corev1.ResourceList{}
				}
				c.Resources.Limits[corev1.ResourceMemory] = override.MemoryLimit
			}
		}
	}
}
