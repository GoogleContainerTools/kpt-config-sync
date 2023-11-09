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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +genclient
// +genclient:nonNamespaced
// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster

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
}

// +kubebuilder:object:root=true

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

// HierarchyModeType defines hierarchical behavior for namespaced objects.
type HierarchyModeType string

const (
	// HierarchyModeInherit indicates that the resource can appear in abstract namespace directories
	// and will be inherited by any descendent namespaces. Without this value on the Sync, resources
	// must not appear in abstract namespaces.
	HierarchyModeInherit = HierarchyModeType("inherit")
	// HierarchyModeNone indicates that the resource cannot appear in abstract namespace directories.
	// For most resource types, this is the same as default, and it's not necessary to specify this
	// value. But RoleBinding and ResourceQuota have different default behaviors, and this value is
	// used to disable inheritance behaviors for those types.
	HierarchyModeNone = HierarchyModeType("none")
	// HierarchyModeDefault is the default value. Default behavior is type-specific.
	HierarchyModeDefault = HierarchyModeType("")
)
