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
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

// ReconcilerContainerLogLevelDefaults are the default log level to use for the
// reconciler deployment containers.
// All containers default value are 0 except git-sync which default value is 5
func ReconcilerContainerLogLevelDefaults() map[string]v1beta1.ContainerLogLevelOverride {
	return map[string]v1beta1.ContainerLogLevelOverride{
		reconcilermanager.Reconciler: {
			ContainerName: reconcilermanager.Reconciler,
			LogLevel:      0,
		},
		reconcilermanager.HydrationController: {
			ContainerName: reconcilermanager.HydrationController,
			LogLevel:      0,
		},
		reconcilermanager.OciSync: {
			ContainerName: reconcilermanager.OciSync,
			LogLevel:      0,
		},
		reconcilermanager.HelmSync: {
			ContainerName: reconcilermanager.HelmSync,
			LogLevel:      0,
		},
		reconcilermanager.GitSync: {
			ContainerName: reconcilermanager.GitSync,
			LogLevel:      5, // git-sync default is 5 so the logs will store git commands ran
		},
		reconcilermanager.GCENodeAskpassSidecar: {
			ContainerName: reconcilermanager.GCENodeAskpassSidecar,
			LogLevel:      0,
		},
		metrics.OtelAgentName: {
			ContainerName: metrics.OtelAgentName,
			LogLevel:      0,
		},
	}
}

// setContainerLogLevelDefaults will compile the default and override value of container log level
func setContainerLogLevelDefaults(overrides []v1beta1.ContainerLogLevelOverride, defaultsMap map[string]v1beta1.ContainerLogLevelOverride) []v1beta1.ContainerLogLevelOverride {

	// copy defaultsMap to local overrideMap
	overrideMap := make(map[string]v1beta1.ContainerLogLevelOverride)
	for containerName, logLevelOverride := range defaultsMap {
		overrideMap[containerName] = v1beta1.ContainerLogLevelOverride{
			ContainerName: logLevelOverride.ContainerName,
			LogLevel:      logLevelOverride.LogLevel,
		}
	}

	// replace overrideMap value with values from overrides
	for _, override := range overrides {
		overrideMap[override.ContainerName] = v1beta1.ContainerLogLevelOverride{
			ContainerName: override.ContainerName,
			LogLevel:      override.LogLevel,
		}
	}

	// convert overrideMap back to list
	overrideList := make([]v1beta1.ContainerLogLevelOverride, 0, len(overrideMap))
	for _, override := range overrideMap {
		overrideList = append(overrideList, override)
	}

	return overrideList
}

// mutateContainerLogLevel will add log level to container args as specified in the override
func mutateContainerLogLevel(c *corev1.Container, override []v1beta1.ContainerLogLevelOverride) {
	if len(override) == 0 {
		return
	}
	for i, arg := range c.Args {
		if strings.HasPrefix(arg, "-v=") {
			c.Args = removeArg(c.Args, i)
			break
		}
	}

	for _, logLevel := range override {
		if logLevel.ContainerName == c.Name {
			c.Args = append(c.Args, fmt.Sprintf("-v=%d", logLevel.LogLevel))
			break
		}
	}
}
