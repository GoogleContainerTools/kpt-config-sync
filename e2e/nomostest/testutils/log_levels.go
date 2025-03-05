// Copyright 2025 Google LLC
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

package testutils

import (
	"fmt"
	"slices"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
)

// UpdateRootSyncReconcilerLogLevel updates the LogLevel for a specific RootSync
// container and waits for the RootSync to reconcile.
//
// Warning: If the RootSync is managed by another RootSync, this change will
// likely be reverted and could hang until watch timeout and then error.
// Update the RootSync in the source of truth instead.
func UpdateRootSyncReconcilerLogLevel(nt *nomostest.NT, syncName, containerName string, logLevel int) error {
	obj := k8sobjects.RootSyncObjectV1Beta1(syncName)
	if err := nt.KubeClient.Get(obj.Name, obj.Namespace, obj); err != nil {
		return err
	}
	if !setRootSyncReconcilerLogLevel(obj, containerName, logLevel) {
		return nil // no-op
	}
	if err := nt.KubeClient.Update(obj); err != nil {
		return err
	}
	nt.T.Cleanup(func() {
		obj := k8sobjects.RootSyncObjectV1Beta1(syncName)
		nt.Must(nt.KubeClient.Get(obj.Name, obj.Namespace, obj))
		if !resetRootSyncReconcilerLogLevel(obj, containerName) {
			return // no-op
		}
		nt.Must(nt.KubeClient.Update(obj))
	})
	return nt.Watcher.WatchForCurrentStatus(kinds.RootSyncV1Beta1(), obj.Name, obj.Namespace)
}

func setRootSyncReconcilerLogLevel(obj *v1beta1.RootSync, containerName string, logLevel int) bool {
	if obj.Spec.Override == nil {
		obj.Spec.SafeOverride().LogLevels = []v1beta1.ContainerLogLevelOverride{
			{ContainerName: containerName, LogLevel: logLevel},
		}
		return true
	}
	updated := false
	for l, override := range obj.Spec.Override.LogLevels {
		if override.ContainerName == containerName {
			if override.LogLevel == logLevel {
				return false // no-op
			}
			obj.Spec.Override.LogLevels[l].LogLevel = logLevel
			updated = true
		}
	}
	if !updated {
		obj.Spec.Override.LogLevels = append(obj.Spec.Override.LogLevels,
			v1beta1.ContainerLogLevelOverride{
				ContainerName: containerName,
				LogLevel:      logLevel,
			})
		updated = true
	}
	return updated
}

func resetRootSyncReconcilerLogLevel(obj *v1beta1.RootSync, containerName string) bool {
	if obj.Spec.Override == nil {
		return false // no-op
	}
	updated := false
	for l, override := range obj.Spec.Override.LogLevels {
		if override.ContainerName == containerName {
			obj.Spec.Override.LogLevels = slices.Delete(
				obj.Spec.Override.LogLevels, l, l+1)
			updated = true
			break
		}
	}
	return updated
}

// UpdateDeploymentContainerVerbosityArg updates the verbosity argument for a
// specific Deployment container and waits for the Deployment to reconcile.
//
// Warning: If the Deployment is managed by Config Sync, this change will
// likely be reverted and could hang until watch timeout and then error.
// Update the Deployment in the source of truth instead.
func UpdateDeploymentContainerVerbosityArg(nt *nomostest.NT, name, namespace, containerName string, logLevel int) error {
	obj := k8sobjects.DeploymentObject(
		core.Name(name),
		core.Namespace(namespace))
	if err := nt.KubeClient.Get(obj.Name, obj.Namespace, obj); err != nil {
		return err
	}
	if !setDeploymentContainerVerbosity(obj, containerName, logLevel) {
		return nil //no-op
	}
	if err := nt.KubeClient.Update(obj); err != nil {
		return err
	}
	nt.T.Cleanup(func() {
		obj := k8sobjects.DeploymentObject(
			core.Name(name),
			core.Namespace(namespace))
		nt.Must(nt.KubeClient.Get(obj.Name, obj.Namespace, obj))
		if !resetDeploymentContainerVerbosity(obj, containerName) {
			return //no-op
		}
		nt.Must(nt.KubeClient.Update(obj))
	})
	return nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), obj.Name, obj.Namespace)
}

func setDeploymentContainerVerbosity(obj *appsv1.Deployment, containerName string, logLevel int) bool {
	vArg := fmt.Sprintf("-v=%d", logLevel)
	updated := false
	for c, container := range obj.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			for a, arg := range container.Args {
				if arg == vArg {
					return false // no-op
				}
				if strings.HasPrefix(arg, "-v=") {
					obj.Spec.Template.Spec.Containers[c].Args[a] = vArg
					updated = true
					break
				}
			}
			if !updated {
				obj.Spec.Template.Spec.Containers[c].Args = append(
					obj.Spec.Template.Spec.Containers[c].Args, vArg)
				updated = true
			}
			break
		}
		// Note: Does not error if the container is not found
	}
	return updated
}

func resetDeploymentContainerVerbosity(obj *appsv1.Deployment, containerName string) bool {
	updated := false
	for c, container := range obj.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			for a, arg := range container.Args {
				if strings.HasPrefix(arg, "-v=") {
					obj.Spec.Template.Spec.Containers[c].Args = slices.Delete(
						obj.Spec.Template.Spec.Containers[c].Args, a, a+1)
					updated = true
					break
				}
			}
			break
		}
		// Note: Does not error if the container is not found
	}
	return updated
}
