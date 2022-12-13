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

package v1alpha1

import (
	"reflect"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Reconciling",type="string",JSONPath=".status.conditions[0].status"
// +kubebuilder:printcolumn:name="Stalled",type="string",JSONPath=".status.conditions[1].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// ResourceGroup is the Schema for the resourcegroups API
type ResourceGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ResourceGroupSpec   `json:"spec,omitempty"`
	Status ResourceGroupStatus `json:"status,omitempty"`
}

// spec defines the desired state of ResourceGroup
type ResourceGroupSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// resources contains a list of resources that form the resource group
	// +optional
	Resources []ObjMetadata `json:"resources,omitempty"`

	// subgroups contains a list of sub groups that the current group includes.
	// +optional
	Subgroups []GroupMetadata `json:"subgroups,omitempty"`

	// descriptor regroups the information and metadata about a resource group
	// +optional
	Descriptor Descriptor `json:"descriptor,omitempty"`
}

// status defines the observed state of ResourceGroup
type ResourceGroupStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// observedGeneration is the most recent generation observed.
	// It corresponds to the Object's generation, which is updated on
	// mutation by the API Server.
	// Everytime the controller does a successful reconcile, it sets
	// observedGeneration to match ResourceGroup.metadata.generation.
	ObservedGeneration int64 `json:"observedGeneration"`

	// resourceStatuses lists the status for each resource in the group
	ResourceStatuses []ResourceStatus `json:"resourceStatuses,omitempty"`

	// subgroupStatuses lists the status for each subgroup.
	SubgroupStatuses []GroupStatus `json:"subgroupStatuses,omitempty"`

	// conditions lists the conditions of the current status for the group
	Conditions []Condition `json:"conditions,omitempty"`
}

// each item organizes and stores the identifying information
// for an object. This struct (as a string) is stored in a
// grouping object to keep track of sets of applied objects.
type ObjMetadata struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	GroupKind `json:",inline"`
}

// Each item organizes and stores the identifying information
// for a ResourceGroup object. It includes name and namespace.
type GroupMetadata struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type GroupKind struct {
	Group string `json:"group"`
	Kind  string `json:"kind"`
}

type Descriptor struct {
	// type can contain prefix, such as Application/WordPress or Service/Spanner
	// +optional
	Type string `json:"type,omitempty"`

	// revision is an optional revision for a group of resources
	// +optional
	Revision string `json:"revision,omitempty"`

	// description is a brief description of a group of resources
	// +optional
	Description string `json:"description,omitempty"`

	// links are a list of descriptive URLs intended to be used to surface
	// additional information
	// +optional
	Links []Link `json:"links,omitempty"`
}

type Link struct {
	// description explains the purpose of the link
	Description string `json:"description"`

	// url is the URL of the link
	URL string `json:"url"`
}

type Condition struct {
	// type of the condition
	Type ConditionType `json:"type"`

	// status of the condition
	Status ConditionStatus `json:"status"`

	// one-word CamelCase reason for the condition’s last transition
	// +optional
	Reason string `json:"reason,omitempty"`

	// human-readable message indicating details about last transition
	// +optional
	Message string `json:"message,omitempty"`

	// last time the condition transit from one status to another
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// IsEmpty checks whether cond is empty
func (cond Condition) IsEmpty() bool {
	return reflect.DeepEqual(cond, Condition{})
}

type ConditionType string

const (
	Reconciling ConditionType = "Reconciling"
	Stalled     ConditionType = "Stalled"
	// Ownership reflects if the current resource
	// reflects the status for the specification in the current inventory object.
	// Since two ResourceGroup CRs may contain the same resource in the inventory list.
	// The resource status listed in .status.resourceStatuses may only reflect
	// the object status in one group, which is identified by the inventory-id.
	// The ConditionStatus values doesn't make too much sense for OwnerShip type.
	// The actual meaning is captured in the condition message.
	Ownership ConditionType = "OwnershipOverlap"
)

type ConditionStatus string

const (
	TrueConditionStatus    ConditionStatus = "True"
	FalseConditionStatus   ConditionStatus = "False"
	UnknownConditionStatus ConditionStatus = "Unknown"
)

const (
	OwnershipUnmatch = "Overlap"
	OwnershipEmpty   = "Unknown"
)

// each item contains the status of a given resource uniquely identified by
// its group, kind, name and namespace.
type ResourceStatus struct {
	ObjMetadata `json:",inline"`
	Status      Status      `json:"status"`
	SourceHash  string      `json:"sourceHash,omitempty"`
	Conditions  []Condition `json:"conditions,omitempty"`
	Strategy    Strategy    `json:"strategy,omitempty"`
	Actuation   Actuation   `json:"actuation,omitempty"`
	Reconcile   Reconcile   `json:"reconcile,omitempty"`
}

// Each item contains the status of a given group uniquely identified by
// its name and namespace.
type GroupStatus struct {
	GroupMetadata `json:",inline"`
	Status        Status      `json:"status"`
	Conditions    []Condition `json:"conditions,omitempty"`
}

// status describes the status of a resource.
type Status string

const (
	InProgress  Status = "InProgress"
	Failed      Status = "Failed"
	Current     Status = "Current"
	Terminating Status = "Terminating"
	NotFound    Status = "NotFound"
	Unknown     Status = "Unknown"
)

// strategy indicates the method of actuation (apply or delete) used or planned to be used.
type Strategy string

const (
	Apply  Strategy = "Apply"
	Delete Strategy = "Delete"
)

// actuation indicates whether actuation has been performed yet and how it went.
type Actuation string

const (
	ActuationPending   Actuation = "Pending"
	ActuationSucceeded Actuation = "Succeeded"
	ActuationSkipped   Actuation = "Skipped"
	ActuationFailed    Actuation = "Failed"
)

// reconcile indicates whether reconciliation has been performed yet and how it went.
type Reconcile string

const (
	ReconcilePending   Reconcile = "Pending"
	ReconcileSucceeded Reconcile = "Succeeded"
	ReconcileSkipped   Reconcile = "Skipped"
	ReconcileTimeout   Reconcile = "Timeout"
	ReconcileFailed    Reconcile = "Failed"
)

// GK returns a schema.GroupKind object for m
func (m ObjMetadata) GK() schema.GroupKind {
	return schema.GroupKind{
		Group: m.Group,
		Kind:  m.Kind,
	}
}

func ToObjMetadata(groups []GroupMetadata) []ObjMetadata {
	metas := make([]ObjMetadata, len(groups))
	for i, meta := range groups {
		metas[i] = ObjMetadata{
			Namespace: meta.Namespace,
			Name:      meta.Name,
			GroupKind: SubgroupGroupKind,
		}
	}
	return metas
}

func ToGroupStatuses(statuses []ResourceStatus) []GroupStatus {
	groupStatuses := make([]GroupStatus, len(statuses))
	for i, status := range statuses {
		groupStatuses[i] = GroupStatus{
			GroupMetadata: GroupMetadata{
				Name:      status.Name,
				Namespace: status.Namespace,
			},
			Status:     status.Status,
			Conditions: status.Conditions,
		}
	}
	return groupStatuses
}

// +kubebuilder:object:root=true

// ResourceGroupList contains a list of ResourceGroup
type ResourceGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ResourceGroup `json:"items"`
}

//nolint:gochecknoinits // kubebuilder convention for api packages
func init() {
	SchemeBuilder.Register(&ResourceGroup{}, &ResourceGroupList{})
}
