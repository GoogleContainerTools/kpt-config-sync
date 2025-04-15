// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package actuation

import (
	"fmt"

	"k8s.io/apimachinery/pkg/types"
)

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

// ActuationStrategy defines the actuation strategy used for a specific object.
//
//nolint:revive // consistent prefix improves tab-completion for enums
type ActuationStrategy string

// String implements the fmt.Stringer interface.
func (s ActuationStrategy) String() string {
	return string(s)
}

// ParseActuationStrategy parses from string to ActuationStrategy
func ParseActuationStrategy(data string) (ActuationStrategy, error) {
	switch data {
	case ActuationStrategyApply.String():
		return ActuationStrategyApply, nil
	case ActuationStrategyDelete.String():
		return ActuationStrategyDelete, nil
	case ActuationStrategyUnknown.String():
		return ActuationStrategyUnknown, nil
	default:
		return ActuationStrategyUnknown,
			fmt.Errorf("invalid actuation strategy %q: must be %q, %q, or %q",
				data, ActuationStrategyApply, ActuationStrategyDelete, ActuationStrategyUnknown)
	}
}

const (
	ActuationStrategyUnknown ActuationStrategy = ""
	ActuationStrategyApply   ActuationStrategy = "Apply"
	ActuationStrategyDelete  ActuationStrategy = "Delete"
)

// ActuationStatus defines the status of the actuation of a specific object.
//
//nolint:revive // consistent prefix improves tab-completion for enums
type ActuationStatus string

// String implements the fmt.Stringer interface.
func (s ActuationStatus) String() string {
	return string(s)
}

// ParseActuationStatus parses from string to ActuationStatus
func ParseActuationStatus(data string) (ActuationStatus, error) {
	switch data {
	case ActuationPending.String():
		return ActuationPending, nil
	case ActuationSucceeded.String():
		return ActuationSucceeded, nil
	case ActuationSkipped.String():
		return ActuationSkipped, nil
	case ActuationFailed.String():
		return ActuationFailed, nil
	case ActuationUnknown.String():
		return ActuationUnknown, nil
	default:
		return ActuationUnknown,
			fmt.Errorf("invalid actuation status %q: must be one of %q, %q, %q, %q, or %q",
				data, ActuationPending, ActuationSucceeded, ActuationSkipped, ActuationFailed, ActuationUnknown)
	}
}

const (
	ActuationUnknown   ActuationStatus = ""
	ActuationPending   ActuationStatus = "Pending"
	ActuationSucceeded ActuationStatus = "Succeeded"
	ActuationSkipped   ActuationStatus = "Skipped"
	ActuationFailed    ActuationStatus = "Failed"
)

// ReconcileStatus defines the status of the reconciliation of a specific object.
type ReconcileStatus string

// String implements the fmt.Stringer interface.
func (s ReconcileStatus) String() string {
	return string(s)
}

// ParseReconcileStatus parses from string to ReconcileStatus
func ParseReconcileStatus(data string) (ReconcileStatus, error) {
	switch data {
	case ReconcilePending.String():
		return ReconcilePending, nil
	case ReconcileSucceeded.String():
		return ReconcileSucceeded, nil
	case ReconcileSkipped.String():
		return ReconcileSkipped, nil
	case ReconcileFailed.String():
		return ReconcileFailed, nil
	case ReconcileTimeout.String():
		return ReconcileTimeout, nil
	case ReconcileUnknown.String():
		return ReconcileUnknown, nil
	default:
		return ReconcileUnknown,
			fmt.Errorf("invalid reconcile status %q: must be one of %q, %q, %q, %q, %q, or %q",
				data, ReconcilePending, ReconcileSucceeded, ReconcileSkipped, ReconcileFailed, ReconcileTimeout, ReconcileUnknown)
	}
}

const (
	ReconcileUnknown   ReconcileStatus = ""
	ReconcilePending   ReconcileStatus = "Pending"
	ReconcileSucceeded ReconcileStatus = "Succeeded"
	ReconcileSkipped   ReconcileStatus = "Skipped"
	ReconcileFailed    ReconcileStatus = "Failed"
	ReconcileTimeout   ReconcileStatus = "Timeout"
)
