// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package event

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	pollevent "sigs.k8s.io/cli-utils/pkg/kstatus/polling/event"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Type determines the type of events that are available.
//go:generate stringer -type=Type
type Type int

const (
	InitType Type = iota
	ErrorType
	ActionGroupType
	ApplyType
	StatusType
	PruneType
	DeleteType
	WaitType
	ValidationType
)

// Event is the type of the objects that will be returned through
// the channel that is returned from a call to Run. It contains
// information about progress and errors encountered during
// the process of doing apply, waiting for status and doing a prune.
type Event struct {
	// Type is the type of event.
	Type Type

	// InitEvent contains information about which resources will
	// be applied/pruned.
	InitEvent InitEvent

	// ErrorEvent contains information about any errors encountered.
	ErrorEvent ErrorEvent

	// ActionGroupEvent contains information about the progression of tasks
	// to apply, prune, and destroy resources, and tasks that involves waiting
	// for a set of resources to reach a specific state.
	ActionGroupEvent ActionGroupEvent

	// ApplyEvent contains information about progress pertaining to
	// applying a resource to the cluster.
	ApplyEvent ApplyEvent

	// StatusEvents contains information about the status of one of
	// the applied resources.
	StatusEvent StatusEvent

	// PruneEvent contains information about objects that have been
	// pruned.
	PruneEvent PruneEvent

	// DeleteEvent contains information about object that have been
	// deleted.
	DeleteEvent DeleteEvent

	// WaitEvent contains information about any errors encountered in a WaitTask.
	WaitEvent WaitEvent

	// ValidationEvent contains information about validation errors.
	ValidationEvent ValidationEvent
}

// String returns a string suitable for logging
func (e Event) String() string {
	var sb strings.Builder
	sb.WriteString("Event{ ")
	switch e.Type {
	case InitType:
		sb.WriteString(e.InitEvent.String())
	case ErrorType:
		sb.WriteString(e.ErrorEvent.String())
	case ActionGroupType:
		sb.WriteString(e.ActionGroupEvent.String())
	case ApplyType:
		sb.WriteString(e.ApplyEvent.String())
	case StatusType:
		sb.WriteString(e.StatusEvent.String())
	case PruneType:
		sb.WriteString(e.PruneEvent.String())
	case DeleteType:
		sb.WriteString(e.DeleteEvent.String())
	case WaitType:
		sb.WriteString(e.WaitEvent.String())
	case ValidationType:
		sb.WriteString(e.ValidationEvent.String())
	}
	sb.WriteString(" }")
	return sb.String()
}

type InitEvent struct {
	ActionGroups ActionGroupList
}

// String returns a string suitable for logging
func (ie InitEvent) String() string {
	return fmt.Sprintf("InitEvent{ ActionGroups: %s }", ie.ActionGroups)
}

//go:generate stringer -type=ResourceAction -linecomment
type ResourceAction int

const (
	ApplyAction     ResourceAction = iota // Apply
	PruneAction                           // Prune
	DeleteAction                          // Delete
	WaitAction                            // Wait
	InventoryAction                       // Inventory
)

type ActionGroupList []ActionGroup

// String returns a string suitable for logging
func (agl ActionGroupList) String() string {
	var sb strings.Builder
	sb.WriteString("[ ")
	for i, ag := range agl {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString("[ ")
		sb.WriteString(ag.String())
		sb.WriteString(" ]")
	}
	sb.WriteString(" ]")
	return sb.String()
}

type ActionGroup struct {
	Name        string
	Action      ResourceAction
	Identifiers object.ObjMetadataSet
}

// String returns a string suitable for logging
func (ag ActionGroup) String() string {
	return fmt.Sprintf("ActionGroup{ Name: %q, Action: %q, Identifiers: %s }",
		ag.Name, ag.Action, ag.Identifiers)
}

type ErrorEvent struct {
	Err error
}

// String returns a string suitable for logging
func (ee ErrorEvent) String() string {
	return fmt.Sprintf("ErrorEvent{ Err: %q }", ee.Err.Error())
}

//go:generate stringer -type=WaitEventOperation -linecomment
type WaitEventOperation int

const (
	ReconcilePending WaitEventOperation = iota // Pending
	Reconciled
	ReconcileSkipped // Skipped
	ReconcileTimeout // Timeout
	ReconcileFailed  // Failed
)

type WaitEvent struct {
	GroupName  string
	Identifier object.ObjMetadata
	Operation  WaitEventOperation
}

// String returns a string suitable for logging
func (we WaitEvent) String() string {
	return fmt.Sprintf("WaitEvent{ GroupName: %q, Operation: %q, Identifier: %q }",
		we.GroupName, we.Operation, we.Identifier)
}

//go:generate stringer -type=ActionGroupEventType
type ActionGroupEventType int

const (
	Started ActionGroupEventType = iota
	Finished
)

type ActionGroupEvent struct {
	GroupName string
	Action    ResourceAction
	Type      ActionGroupEventType
}

// String returns a string suitable for logging
func (age ActionGroupEvent) String() string {
	return fmt.Sprintf("ActionGroupEvent{ GroupName: %q, Action: %q, Type: %q }",
		age.GroupName, age.Action, age.Type)
}

//go:generate stringer -type=ApplyEventOperation
type ApplyEventOperation int

const (
	ApplyUnspecified ApplyEventOperation = iota
	ServersideApplied
	Created
	Unchanged
	Configured
)

type ApplyEvent struct {
	GroupName  string
	Identifier object.ObjMetadata
	Operation  ApplyEventOperation
	Resource   *unstructured.Unstructured
	Error      error
}

// String returns a string suitable for logging
func (ae ApplyEvent) String() string {
	return fmt.Sprintf("ApplyEvent{ GroupName: %q, Operation: %q, Identifier: %q, Error: %q }",
		ae.GroupName, ae.Operation, ae.Identifier, ae.Error)
}

type StatusEvent struct {
	Identifier       object.ObjMetadata
	PollResourceInfo *pollevent.ResourceStatus
	Resource         *unstructured.Unstructured
	Error            error
}

// String returns a string suitable for logging
func (se StatusEvent) String() string {
	status := "nil"
	gen := int64(0)
	if se.PollResourceInfo != nil {
		status = se.PollResourceInfo.Status.String()
		if se.PollResourceInfo.Resource != nil {
			gen = se.PollResourceInfo.Resource.GetGeneration()
		}
	}
	return fmt.Sprintf("StatusEvent{ Status: %q, Generation: %d, Identifier: %q, Error: %q }",
		status, gen, se.Identifier, se.Error)
}

//go:generate stringer -type=PruneEventOperation
type PruneEventOperation int

const (
	PruneUnspecified PruneEventOperation = iota
	Pruned
	PruneSkipped
)

type PruneEvent struct {
	GroupName  string
	Identifier object.ObjMetadata
	Operation  PruneEventOperation
	Object     *unstructured.Unstructured
	// If prune is skipped, this reason string explains why
	Reason string
	Error  error
}

// String returns a string suitable for logging
func (pe PruneEvent) String() string {
	return fmt.Sprintf("PruneEvent{ GroupName: %q, Operation: %q, Identifier: %q, Reason: %q, Error: %q }",
		pe.GroupName, pe.Operation, pe.Identifier, pe.Reason, pe.Error)
}

//go:generate stringer -type=DeleteEventOperation
type DeleteEventOperation int

const (
	DeleteUnspecified DeleteEventOperation = iota
	Deleted
	DeleteSkipped
)

type DeleteEvent struct {
	GroupName  string
	Identifier object.ObjMetadata
	Operation  DeleteEventOperation
	Object     *unstructured.Unstructured
	// If delete is skipped, this reason string explains why
	Reason string
	Error  error
}

// String returns a string suitable for logging
func (de DeleteEvent) String() string {
	return fmt.Sprintf("DeleteEvent{ GroupName: %q, Operation: %q, Identifier: %q, Reason: %q, Error: %q }",
		de.GroupName, de.Operation, de.Identifier, de.Reason, de.Error)
}

type ValidationEvent struct {
	Identifiers object.ObjMetadataSet
	Error       error
}

// String returns a string suitable for logging
func (ve ValidationEvent) String() string {
	return fmt.Sprintf("ValidationEvent{ Identifiers: %+v, Error: %q }",
		ve.Identifiers, ve.Error)
}
