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

package v1beta1

import (
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
)

// OverrideSpec allows to override the settings for a reconciler pod
type OverrideSpec struct {
	// resources allow one to override the resource requirements for the containers in a reconciler pod.
	// +optional
	Resources []ContainerResourcesSpec `json:"resources,omitempty"`

	// gitSyncDepth allows one to override the number of git commits to fetch.
	// Must be no less than 0.
	// Config Sync would do a full clone if this field is 0, and a shallow
	// clone if this field is greater than 0.
	// If this field is not provided, Config Sync would configure it automatically.
	//
	// +kubebuilder:validation:Minimum=0
	// +optional
	GitSyncDepth *int64 `json:"gitSyncDepth,omitempty"`

	// statusMode controls whether the actuation status
	// such as apply failed or not should be embedded into the ResourceGroup object.
	// Must be "enabled" or "disabled".
	// If set to "enabled", it increases the size of the ResourceGroup object.
	//
	// +kubebuilder:validation:Pattern=^(enabled|disabled|)$
	// +optional
	StatusMode string `json:"statusMode,omitempty"`

	// reconcileTimeout allows one to override the threshold for how long to wait for
	// all resources to reconcile before giving up.
	// Default: 5m.
	// Use string to specify this field value, like "30s", "5m".
	// More details about valid inputs: https://pkg.go.dev/time#ParseDuration.
	// Recommended reconcileTimeout range is from "10s" to "1h".
	// +optional
	ReconcileTimeout *metav1.Duration `json:"reconcileTimeout,omitempty"`

	// apiServerTimeout allows one to override the client-side timeout for requests to the API server.
	// Default: 15s.
	// Use string to specify this field value, like "30s", "1m".
	// More details about valid inputs: https://pkg.go.dev/time#ParseDuration.
	// Recommended apiServerTimeout range is from "3s" to "1m".
	// +optional
	APIServerTimeout *metav1.Duration `json:"apiServerTimeout,omitempty"`

	// enableShellInRendering specifies whether to enable or disable the shell access in rendering process. Default: false.
	// Kustomize remote bases requires shell access. Setting this field to true will enable shell in the rendering process and
	// support pulling remote bases from public repositories.
	// +optional
	EnableShellInRendering *bool `json:"enableShellInRendering,omitempty"`

	// logLevels specify the container name and log level override value for the reconciler deployment container.
	// Each entry must contain the name of the reconciler deployment container and the desired log level.
	// +listType=map
	// +listMapKey=containerName
	// +optional
	LogLevels []ContainerLogLevelOverride `json:"logLevels,omitempty"`
}

// RootSyncOverrideSpec allows to override the settings for a RootSync reconciler pod
type RootSyncOverrideSpec struct {
	OverrideSpec `json:",inline"`

	// namespaceStrategy controls how the reconciler handles Namespaces
	// which are used by resources in the source but not declared.
	// Must be "implicit" or "explicit"
	// "implicit" means that the reconciler will implicitly create Namespaces
	// if they do not exist, even if they are not declared in the source.
	// "explicit" means that the reconciler will not create Namespaces which
	// are not declared in the source.
	//
	// +kubebuilder:validation:Enum=implicit;explicit
	// +optional
	NamespaceStrategy configsync.NamespaceStrategy `json:"namespaceStrategy,omitempty"`
}

// RepoSyncOverrideSpec allows to override the settings for a RepoSync reconciler pod
type RepoSyncOverrideSpec struct {
	OverrideSpec `json:",inline"`
}

// ContainerResourcesSpec allows to override the resource requirements for a container
type ContainerResourcesSpec struct {
	// containerName specifies the name of a container whose resource requirements will be overridden.
	// Must be "reconciler", "git-sync", "hydration-controller", "oci-sync", or "helm-sync".
	//
	// +kubebuilder:validation:Pattern=^(reconciler|git-sync|hydration-controller|oci-sync|helm-sync|gcenode-askpass-sidecar)$
	// +optional
	ContainerName string `json:"containerName,omitempty"`
	// cpuRequest allows one to override the CPU request of a container
	// +optional
	CPURequest resource.Quantity `json:"cpuRequest,omitempty"`
	// memoryRequest allows one to override the memory request of a container
	// +optional
	MemoryRequest resource.Quantity `json:"memoryRequest,omitempty"`
	// cpuLimit allows one to override the CPU limit of a container
	// +optional
	CPULimit resource.Quantity `json:"cpuLimit,omitempty"`
	// memoryLimit allows one to override the memory limit of a container
	// +optional
	MemoryLimit resource.Quantity `json:"memoryLimit,omitempty"`
}

// ContainerLogLevelOverride specifies the container name and log level override value
type ContainerLogLevelOverride struct {
	// containerName specifies the name of the reconciler deployment container for which log level will be overridden.
	// Must be one of the following: "reconciler", "git-sync", "hydration-controller", "oci-sync", or "helm-sync".
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^(reconciler|git-sync|hydration-controller|oci-sync|helm-sync|gcenode-askpass-sidecar)$
	ContainerName string `json:"containerName"`

	// logLevel specifies the log level override value for the container.
	// The default value for git-sync container is 5, while all other containers will default to 0.
	// Allowed values are from 0 to 10
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	// +kubebuilder:validation:Required
	LogLevel int `json:"logLevel"`
}
