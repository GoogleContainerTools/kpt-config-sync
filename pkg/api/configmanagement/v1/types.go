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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// These comments must remain outside the package docstring.
// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterSelector specifies a LabelSelector applied to clusters that exist within a
// cluster registry.
type ClusterSelector struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// The actual object definition, per K8S object definition style.
	Spec ClusterSelectorSpec `json:"spec"`
}

// ClusterSelectorSpec contains spec fields for ClusterSelector.
type ClusterSelectorSpec struct {
	// Selects clusters.
	// This field is NOT optional and follows standard label selector semantics. An empty selector
	// matches all clusters.
	Selector metav1.LabelSelector `json:"selector"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ClusterSelectorList holds a list of ClusterSelector resources.
type ClusterSelectorList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of selectors.
	Items []ClusterSelector `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NamespaceSelector specifies a LabelSelector applied to namespaces that exist within a
// NamespaceConfig hierarchy.
type NamespaceSelector struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata. The Name field of the config must match the namespace name.
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// The actual object definition, per K8S object definition style.
	Spec NamespaceSelectorSpec `json:"spec"`
}

// NamespaceSelectorSpec contains spec fields for NamespaceSelector.
type NamespaceSelectorSpec struct {
	// Selects namespaces.
	// This field is NOT optional and follows standard label selector semantics. An empty selector
	// matches all namespaces.
	Selector metav1.LabelSelector `json:"selector"`

	// mode specifies the selection mode of the NamespaceSelector.
	// It must be set to either "static" or "dynamic" and is optional. If not specified, it defaults to "static."
	// In static mode, only resources with labels matching Namespaces statically declared in the source of truth are selected.
	// In dynamic mode, selection includes both statically declared Namespaces and Namespaces present on the cluster.
	// +kubebuilder:validation:Pattern=^(static|dynamic)$
	// +kubebuilder:default:=static
	// +optional
	Mode string `json:"mode,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NamespaceSelectorList holds a list of NamespaceSelector resources.
type NamespaceSelectorList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of NamespaceSelectors.
	Items []NamespaceSelector `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

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

	// Code is the error code of this particular error.  Error codes are numeric strings,
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
	ResourceGVK GroupVersionKind `json:"resourceGVK"`
}

// GroupVersionKind identifies a Kind. It substitutes schema.GroupVersionKind with json tags.
type GroupVersionKind struct {
	Group   string `json:"group"`
	Version string `json:"version"`
	Kind    string `json:"kind"`
}

// ParseSchemaGVK parses the schema.GroupVersionKind into custom groupVersionKind with json tags.
func ParseSchemaGVK(gvk schema.GroupVersionKind) GroupVersionKind {
	return GroupVersionKind{
		Group:   gvk.Group,
		Version: gvk.Version,
		Kind:    gvk.Kind,
	}
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// RepoList holds a list of Repo resources.
type RepoList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of Repo declarations.
	Items []Repo `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HierarchyConfig is used for configuring the HierarchyModeType for managed resources.
type HierarchyConfig struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata. The Name field of the config must match the namespace name.
	// +optional
	metav1.ObjectMeta `json:"metadata"`

	// Spec is the standard spec field.
	Spec HierarchyConfigSpec `json:"spec"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HierarchyConfigList holds a list of HierarchyConfig resources.
type HierarchyConfigList struct {
	metav1.TypeMeta `json:",inline"`

	// Standard object's metadata.
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	// Items is a list of HierarchyConfigs.
	Items []HierarchyConfig `json:"items"`
}

// HierarchyConfigSpec specifies the HierarchyConfigResources.
type HierarchyConfigSpec struct {
	Resources []HierarchyConfigResource `json:"resources"`
}

// HierarchyConfigResource specifies the HierarchyModeType based on the matching Groups and Kinds enabled.
type HierarchyConfigResource struct {
	// Group is the name of the APIGroup that contains the resources.
	// +optional
	Group string `json:"group,omitempty"`
	// Kinds is a list of kinds this rule applies to.
	// +optional
	Kinds []string `json:"kinds,omitempty"`
	// HierarchyMode specifies how the object is treated when it appears in an abstract namespace.
	// The default is "inherit", meaning objects are inherited from parent abstract namespaces.
	// If set to "none", the type is not allowed in Abstract Namespaces.
	// +optional
	HierarchyMode HierarchyModeType `json:"hierarchyMode,omitempty"`
}
