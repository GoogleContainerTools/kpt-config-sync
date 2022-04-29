// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package actuation

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Inventory represents the inventory object in memory.
// Inventory is currently only used for in-memory storage and not serialized to
// disk or to the API server.
type Inventory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InventorySpec   `json:"spec,omitempty"`
	Status InventoryStatus `json:"status,omitempty"`
}

// InventorySpec is the specification of the desired/expected inventory state.
type InventorySpec struct {
	Objects []ObjectReference `json:"objects,omitempty"`
}

// InventoryStatus is the status of the current/last-known inventory state.
type InventoryStatus struct {
	Objects []ObjectStatus `json:"objects,omitempty"`
}

// ObjectReference is a reference to a KRM resource by name and kind.
//
// Kubernetes only stores one API Version for each Kind at any given time,
// so version is not used when referencing objects.
type ObjectReference struct {
	// Kind identifies a REST resource within a Group.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
	Kind string `json:"kind,omitempty"`

	// Group identifies an API namespace for REST resources.
	// If group is omitted, it is treated as the "core" group.
	// More info: https://kubernetes.io/docs/reference/using-api/#api-groups
	// +optional
	Group string `json:"group,omitempty"`

	// Name identifies an object instance of a REST resource.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	Name string `json:"name,omitempty"`

	// Namespace identifies a group of objects across REST resources.
	// If namespace is specified, the resource must be namespace-scoped.
	// If namespace is omitted, the resource must be cluster-scoped.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// ObjectStatus is a snapshot of the actuation and reconciliation status of a
// referenced object.
type ObjectStatus struct {
	ObjectReference `json:",inline"`

	// Strategy indicates the method of actuation (apply or delete) used or planned to be used.
	Strategy ActuationStrategy `json:"strategy,omitempty"`
	// Actuation indicates whether actuation has been performed yet and how it went.
	Actuation ActuationStatus `json:"actuation,omitempty"`
	// Reconcile indicates whether reconciliation has been performed yet and how it went.
	Reconcile ReconcileStatus `json:"reconcile,omitempty"`

	// UID is the last known UID (after apply or before delete).
	// This can help identify if the object has been replaced.
	// +optional
	UID types.UID `json:"uid,omitempty"`
	// Generation is the last known Generation (after apply or before delete).
	// This can help identify if the object has been modified.
	// Generation is not available for deleted objects.
	// +optional
	Generation int64 `json:"generation,omitempty"`
}

//nolint:revive // consistent prefix improves tab-completion for enums
//go:generate stringer -type=ActuationStrategy -linecomment
type ActuationStrategy int

const (
	ActuationStrategyApply  ActuationStrategy = iota // Apply
	ActuationStrategyDelete                          // Delete
)

//nolint:revive // consistent prefix improves tab-completion for enums
//go:generate stringer -type=ActuationStatus -linecomment
type ActuationStatus int

const (
	ActuationPending   ActuationStatus = iota // Pending
	ActuationSucceeded                        // Succeeded
	ActuationSkipped                          // Skipped
	ActuationFailed                           // Failed
)

//go:generate stringer -type=ReconcileStatus -linecomment
type ReconcileStatus int

const (
	ReconcilePending   ReconcileStatus = iota // Pending
	ReconcileSucceeded                        // Succeeded
	ReconcileSkipped                          // Skipped
	ReconcileFailed                           // Failed
	ReconcileTimeout                          // Timeout
)
