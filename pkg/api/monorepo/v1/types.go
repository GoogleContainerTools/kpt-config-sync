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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// These comments must remain outside the package docstring.
// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// ClusterConfig is the top-level object for the config data definition.
//
// It holds a config defined for a single org unit (namespace).
type ClusterConfig struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// The actual object definition, per K8S object definition style.
	// +optional
	Spec ClusterConfigSpec `json:"spec,omitempty"`

	// The current status of the object, per K8S object definition style.
	// +optional
	Status ClusterConfigStatus `json:"status,omitempty"`
}

// ClusterConfigSpec defines the configs that will exist at the cluster level.
type ClusterConfigSpec struct {
	// Token indicates the version of the ClusterConfig last imported from the source of truth.
	// +optional
	Token string `json:"token,omitempty"`

	// ImportTime is the timestamp of when the ClusterConfig was updated by the Importer.
	// +optional
	ImportTime metav1.Time `json:"importTime,omitempty"`

	// Resources contains namespace scoped resources that are synced to the API server.
	// +optional
	Resources []GenericResources `json:"resources,omitempty"`
}

// ClusterConfigStatus contains fields that define the status of a ClusterConfig.
type ClusterConfigStatus struct {
	// Token indicates the version of the config that the Syncer last attempted to update from.
	// +optional
	Token string `json:"token,omitempty"`

	// SyncErrors contains any errors that occurred during the last attempt the Syncer made to update
	// resources from the ClusterConfig specs. This field will be empty on success.
	// +optional
	SyncErrors []ConfigManagementError `json:"syncErrors,omitempty"`

	// SyncTime is the timestamp of when the config resources were last updated by the Syncer.
	// +optional
	SyncTime metav1.Time `json:"syncTime,omitempty"`

	// SyncState is the current state of the config resources (eg synced, stale, error).
	// +optional
	SyncState ConfigSyncState `json:"syncState,omitempty"`

	// ResourceConditions contains health status of cluster-scope resources
	// +optional
	ResourceConditions []ResourceCondition `json:"resourceConditions,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterConfigList holds a list of cluster level configs, returned as response to a List call on
// the cluster config hierarchy.
type ClusterConfigList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of configs that apply.
	Items []ClusterConfig `json:"items"`
}

// These comments must remain outside the package docstring.
// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// NamespaceConfig is the top-level object for the config data definition.
//
// It holds a config defined for a single org unit (namespace).
type NamespaceConfig struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata. The Name field of the config must match the namespace name.
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// The actual object definition, per K8S object definition style.
	// +optional
	Spec NamespaceConfigSpec `json:"spec,omitempty"`

	// The current status of the object, per K8S object definition style.
	// +optional
	Status NamespaceConfigStatus `json:"status,omitempty"`
}

// NamespaceConfigSpec contains all the information about a config linkage.
type NamespaceConfigSpec struct {
	// Token indicates the version of the NamespaceConfig last imported from the source of truth.
	// +optional
	Token string `json:"token,omitempty"`

	// ImportTime is the timestamp of when the NamespaceConfig was updated by the Importer.
	// +optional
	ImportTime metav1.Time `json:"importTime,omitempty"`

	// Resources contains namespace scoped resources that are synced to the API server.
	// +optional
	Resources []GenericResources `json:"resources,omitempty"`

	// DeleteSyncedTime is the time at which the importer identified the intent to delete
	// the corresponding Namespace
	// +optional
	DeleteSyncedTime metav1.Time `json:"deleteSyncedTime,omitempty"`
}

// NamespaceConfigStatus contains fields that define the status of a NamespaceConfig.
type NamespaceConfigStatus struct {
	// Token indicates the version of the config that the Syncer last attempted to update from.
	// +optional
	Token string `json:"token,omitempty"`

	// SyncErrors contains any errors that occurred during the last attempt the Syncer made to update
	// resources from the NamespaceConfig specs. This field will be empty on success.
	// +optional
	SyncErrors []ConfigManagementError `json:"syncErrors,omitempty"`

	// SyncTime is the timestamp of when the config resources were last updated by the Syncer.
	// +optional
	SyncTime metav1.Time `json:"syncTime,omitempty"`

	// SyncState is the current state of the config resources (eg synced, stale, error).
	// +optional
	SyncState ConfigSyncState `json:"syncState,omitempty"`

	// ResourceConditions contains health status of namespaced resources
	// +optional
	ResourceConditions []ResourceCondition `json:"resourceConditions,omitempty"`
}

// +kubebuilder:object:root=true

// NamespaceConfigList holds a list of NamespaceConfigs, as response to a List call on the config
// hierarchy API.
type NamespaceConfigList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of configs that apply.
	Items []NamespaceConfig `json:"items"`
}

// GenericResources contains API objects of a specified Group and Kind.
type GenericResources struct {
	// Group is the Group for all resources contained within
	// +optional
	Group string `json:"group,omitempty"`

	// Kind is the Kind for all resources contained within.
	Kind string `json:"kind"`

	// Versions is a list Versions corresponding to the Version for this Group and Kind.
	Versions []GenericVersionResources `json:"versions"` // Per version information.
}

// GenericVersionResources holds a set of resources of a single version for a Group and Kind.
type GenericVersionResources struct {
	// Version is the version of all objects in Objects.
	Version string `json:"version"`

	// Objects is the list of objects of a single Group Version and Kind.
	Objects []runtime.RawExtension `json:"objects"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// Sync is used for configuring sync of generic resources.
type Sync struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata. The Name field of the config must match the namespace name.
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// Spec is the standard spec field.
	Spec SyncSpec `json:"spec"`

	// Status is the status for the sync declaration.
	Status SyncStatus `json:"status,omitempty"`
}

// SyncSpec specifies the sync declaration which corresponds to an API Group and contained
// kinds and versions.
type SyncSpec struct {
	// Group is the group, for example configmanagement.gke.io or rbac.authorization.k8s.io
	Group string `json:"group"` // group, eg configmanagement.gke.io
	// Kind is the string that represents the Kind for the object as given in TypeMeta, for example
	// ClusterRole, Namespace or Deployment.
	Kind string `json:"kind"`
	// HierarchyMode specifies how the object is treated when it appears in an abstract namespace.
	// The default is "inherit", meaning objects are inherited from parent abstract namespaces.
	// If set to "none", the type is not allowed in Abstract Namespaces.
	// +optional
	HierarchyMode v1.HierarchyModeType `json:"hierarchyMode,omitempty"`
}

// SyncStatus represents the status for a sync declaration
type SyncStatus struct {
	// Status indicates the state of the sync.  One of "syncing", or "error".  If "error" is specified
	// then Error will be populated with a message regarding the error.
	Status SyncState `json:"status"`
	// Message indicates a message associated with the status.
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true

// SyncList holds a list of Sync resources.
type SyncList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of sync declarations.
	Items []Sync `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

// Repo holds configuration and status about the Nomos source of truth.
type Repo struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec RepoSpec `json:"spec,omitempty"`

	// +optional
	Status RepoStatus `json:"status,omitempty"`
}

// RepoSpec contains spec fields for Repo.
type RepoSpec struct {
	// Repo version string, corresponds to how the config importer should handle the directory
	// structure (implicit assumptions).
	Version string `json:"version"`
}

// RepoStatus contains status fields for Repo.
type RepoStatus struct {
	// +optional
	Source RepoSourceStatus `json:"source,omitempty"`

	// +optional
	Import RepoImportStatus `json:"import,omitempty"`

	// +optional
	Sync RepoSyncStatus `json:"sync,omitempty"`
}

// RepoSourceStatus contains status fields for the Repo's source of truth.
type RepoSourceStatus struct {
	// Most recent version token seen in the source of truth (eg the repo). This token is updated as
	// soon as the config importer sees a new change in the repo.
	// +optional
	Token string `json:"token,omitempty"`

	// Errors is a list of any errors that occurred while reading from the source of truth.
	// +optional
	Errors []ConfigManagementError `json:"errors,omitempty"`
}

// RepoImportStatus contains status fields for the import of the Repo.
type RepoImportStatus struct {
	// Most recent version token imported from the source of truth into Nomos CRs. This token is
	// updated once the importer finishes processing a change, whether or not there were errors
	// during the import.
	// +optional
	Token string `json:"token,omitempty"`

	// LastUpdate is the timestamp of when this status was updated by the Importer.
	// +optional
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`

	// Errors is a list of any errors that occurred while performing the most recent import indicated
	// by Token.
	// +optional
	Errors []ConfigManagementError `json:"errors,omitempty"`
}

// RepoSyncStatus contains status fields for the sync of the Repo.
type RepoSyncStatus struct {
	// LatestToken is the most recent version token synced from the source of truth to managed K8S
	// resources. This token is updated as soon as the syncer starts processing a new change, whether
	// or not it has finished processing or if there were errors during the sync.
	// +optional
	LatestToken string `json:"latestToken,omitempty"`

	// LastUpdate is the timestamp of when this status was updated by the Importer.
	// +optional
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`

	// InProgress is a list of changes that are currently being synced. Each change may or may not
	// have associated errors.
	// +optional
	InProgress []RepoSyncChangeStatus `json:"inProgress,omitempty"`

	ResourceConditions []ResourceCondition `json:"resourceConditions,omitempty"`
}

// ResourceCondition represents the sync status of the resource
type ResourceCondition struct {
	GroupVersion   string                 `json:"groupVersion,omitempty"`
	Kind           string                 `json:"kind,omitempty"`
	NamespacedName string                 `json:"namespacedName,omitempty"`
	ResourceState  ResourceConditionState `json:"resourceState,omitempty"`
	Token          string                 `json:"token,omitempty"`

	// These fields match the proposed conditions/annotations for status.
	ReconcilingReasons []string `json:"reconcilingReasons,omitempty"`
	Errors             []string `json:"errors,omitempty"`
}

// RepoSyncChangeStatus represents the status of a single change being synced in the Repo.
type RepoSyncChangeStatus struct {
	// Token is the version token for the change being synced from the source of truth to managed K8S
	// resources.
	// +optional
	Token string `json:"token,omitempty"`

	// Errors is a list of any errors that occurred while syncing the resources changed for the
	// version token above.
	// +optional
	Errors []ConfigManagementError `json:"errors,omitempty"`
}

// ConfigManagementError represents an error that occurs during the management of configs. It is
// typically produced when processing the source of truth, importing a config, or syncing a K8S
// resource.
type ConfigManagementError struct {
	// ErrorResource is unused and should be removed when we uprev the API version.
	ErrorResource `json:",inline"`

	// Code is the error code of this particualr error.  Error codes are numeric strings,
	// like "1012".
	// +optional
	Code string `json:"code"`

	// ErrorMessage describes the error that occurred.
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// ErrorResourcs describes the resources associated with this error, if any.
	ErrorResources []ErrorResource `json:"errorResources,omitempty"`
}

// ErrorResource contains the identification bits of a single resource that is involved in
// a resource error.
type ErrorResource struct {
	// SourcePath is the repo-relative slash path to where the config is defined. This field may be
	// empty for errors that are not associated with a specific config file.
	// +optional
	SourcePath string `json:"sourcePath,omitempty"`

	// ResourceName is the name of the affected K8S resource. This field may be empty for errors that
	// are not associated with a specific resource.
	// +optional
	ResourceName string `json:"resourceName,omitempty"`

	// ResourceNamespace is the namespace of the affected K8S resource. This field may be empty for
	// errors that are associated with a cluster-scoped resource or not associated with a specific
	// resource.
	// +optional
	ResourceNamespace string `json:"resourceNamespace,omitempty"`

	// ResourceGVK is the GroupVersionKind of the affected K8S resource. This field may be empty for
	// errors that are not associated with a specific resource.
	// +optional
	ResourceGVK schema.GroupVersionKind `json:"resourceGVK"`
}

// +kubebuilder:object:root=true

// RepoList holds a list of Repo resources.
type RepoList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of Repo declarations.
	Items []Repo `json:"items"`
}

// AddResource adds a client.Object to this ClusterConfig.
func (c *ClusterConfig) AddResource(o client.Object) {
	c.Spec.Resources = appendResource(c.Spec.Resources, o)
}

// AddResource adds a client.Object to this NamespaceConfig.
func (c *NamespaceConfig) AddResource(o client.Object) {
	c.Spec.Resources = appendResource(c.Spec.Resources, o)
}

// appendResource adds Object o to resources.
// GenericResources is grouped first by kind and then by version, and this method takes care of
// adding any required groupings for the new object, or adding to existing groupings if present.
func appendResource(resources []GenericResources, o client.Object) []GenericResources {
	gvk := o.GetObjectKind().GroupVersionKind()
	var gr *GenericResources
	for i := range resources {
		if resources[i].Group == gvk.Group && resources[i].Kind == gvk.Kind {
			gr = &resources[i]
			break
		}
	}
	if gr == nil {
		resources = append(resources, GenericResources{
			Group: gvk.Group,
			Kind:  gvk.Kind,
		})
		gr = &resources[len(resources)-1]
	}
	var gvr *GenericVersionResources
	for i := range gr.Versions {
		if gr.Versions[i].Version == gvk.Version {
			gvr = &gr.Versions[i]
			break
		}
	}
	if gvr == nil {
		gr.Versions = append(gr.Versions, GenericVersionResources{
			Version: gvk.Version,
		})
		gvr = &gr.Versions[len(gr.Versions)-1]
	}
	gvr.Objects = append(gvr.Objects, runtime.RawExtension{Object: o})
	return resources
}
