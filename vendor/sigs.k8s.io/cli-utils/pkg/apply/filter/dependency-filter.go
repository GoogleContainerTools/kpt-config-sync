// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply/taskrunner"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
)

//go:generate stringer -type=Relationship -linecomment
type Relationship int

const (
	RelationshipDependent  Relationship = iota // Dependent
	RelationshipDependency                     // Dependency
)

//go:generate stringer -type=Phase -linecomment
type Phase int

const (
	PhaseActuation Phase = iota // Actuation
	PhaseReconcile              // Reconcile
)

// DependencyFilter implements ValidationFilter interface to determine if an
// object can be applied or deleted based on the status of it's dependencies.
type DependencyFilter struct {
	TaskContext       *taskrunner.TaskContext
	ActuationStrategy actuation.ActuationStrategy
	DryRunStrategy    common.DryRunStrategy
}

const DependencyFilterName = "DependencyFilter"

// Name returns the name of the filter for logs and events.
func (dnrf DependencyFilter) Name() string {
	return DependencyFilterName
}

// Filter returns an error if the specified object should be skipped because at
// least one of its dependencies is Not Found or Not Reconciled.
// Typed Errors:
// - DependencyPreventedActuationError
// - DependencyActuationMismatchError
func (dnrf DependencyFilter) Filter(obj *unstructured.Unstructured) error {
	id := object.UnstructuredToObjMetadata(obj)

	switch dnrf.ActuationStrategy {
	case actuation.ActuationStrategyApply:
		// For apply, check dependencies (outgoing)
		for _, depID := range dnrf.TaskContext.Graph().Dependencies(id) {
			err := dnrf.filterByRelationship(id, depID, RelationshipDependency)
			if err != nil {
				return err
			}
		}
	case actuation.ActuationStrategyDelete:
		// For delete, check dependents (incoming)
		for _, depID := range dnrf.TaskContext.Graph().Dependents(id) {
			err := dnrf.filterByRelationship(id, depID, RelationshipDependent)
			if err != nil {
				return err
			}
		}
	default:
		return NewFatalError(fmt.Errorf("invalid actuation strategy: %q", dnrf.ActuationStrategy))
	}
	return nil
}

func (dnrf DependencyFilter) filterByRelationship(aID, bID object.ObjMetadata, relationship Relationship) error {
	// Dependency on an invalid object is considered an invalid dependency, making both objects invalid.
	// For applies: don't prematurely apply something that depends on something that hasn't been applied (because invalid).
	// For deletes: don't prematurely delete something that depends on something that hasn't been deleted (because invalid).
	// These can't be caught be subsequent checks, because invalid objects aren't in the inventory.
	if dnrf.TaskContext.IsInvalidObject(bID) {
		// Should have been caught in validation
		return NewFatalError(fmt.Errorf("invalid %s: %s",
			strings.ToLower(relationship.String()),
			bID))
	}

	status, found := dnrf.TaskContext.InventoryManager().ObjectStatus(bID)
	if !found {
		// Status is registered during planning.
		// So if status is not found, the object is external (NYI) or invalid.
		// Should have been caught in validation.
		return NewFatalError(fmt.Errorf("unknown %s actuation strategy: %s",
			strings.ToLower(relationship.String()), bID))
	}

	// Dependencies must have the same actuation strategy.
	// If there is a mismatch, skip both.
	if status.Strategy != dnrf.ActuationStrategy {
		// Skip!
		return &DependencyActuationMismatchError{
			Object:           aID,
			Strategy:         dnrf.ActuationStrategy,
			Relationship:     relationship,
			Relation:         bID,
			RelationStrategy: status.Strategy,
		}
	}

	switch status.Actuation {
	case actuation.ActuationPending:
		// If actuation is still pending, dependency sorting is probably broken.
		return NewFatalError(fmt.Errorf("premature %s: %s %s actuation %s: %s",
			strings.ToLower(dnrf.ActuationStrategy.String()),
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Strategy.String()),
			strings.ToLower(status.Actuation.String()),
			bID))
	case actuation.ActuationSkipped, actuation.ActuationFailed:
		// Skip!
		return &DependencyPreventedActuationError{
			Object:                  aID,
			Strategy:                dnrf.ActuationStrategy,
			Relationship:            relationship,
			Relation:                bID,
			RelationPhase:           PhaseActuation,
			RelationActuationStatus: status.Actuation,
			RelationReconcileStatus: status.Reconcile,
		}
	case actuation.ActuationSucceeded:
		// Don't skip!
	default:
		// Should never happen
		return NewFatalError(fmt.Errorf("invalid %s actuation status %q: %s",
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Actuation.String()),
			bID))
	}

	// DryRun skips WaitTasks, so reconcile status can be ignored
	if dnrf.DryRunStrategy.ClientOrServerDryRun() {
		// Don't skip!
		return nil
	}

	switch status.Reconcile {
	case actuation.ReconcilePending:
		// If reconcile is still pending, dependency sorting is probably broken.
		return NewFatalError(fmt.Errorf("premature %s: %s %s reconcile %s: %s",
			strings.ToLower(dnrf.ActuationStrategy.String()),
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Strategy.String()),
			strings.ToLower(status.Reconcile.String()),
			bID))
	case actuation.ReconcileSkipped, actuation.ReconcileFailed, actuation.ReconcileTimeout:
		// Skip!
		return &DependencyPreventedActuationError{
			Object:                  aID,
			Strategy:                dnrf.ActuationStrategy,
			Relationship:            relationship,
			Relation:                bID,
			RelationPhase:           PhaseReconcile,
			RelationActuationStatus: status.Actuation,
			RelationReconcileStatus: status.Reconcile,
		}
	case actuation.ReconcileSucceeded:
		// Don't skip!
	default:
		// Should never happen
		return NewFatalError(fmt.Errorf("invalid %s reconcile status %q: %s",
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Reconcile.String()),
			bID))
	}

	// Don't skip!
	return nil
}

type DependencyPreventedActuationError struct {
	Object       object.ObjMetadata
	Strategy     actuation.ActuationStrategy
	Relationship Relationship

	Relation                object.ObjMetadata
	RelationPhase           Phase
	RelationActuationStatus actuation.ActuationStatus
	RelationReconcileStatus actuation.ReconcileStatus
}

func (e *DependencyPreventedActuationError) Error() string {
	switch e.RelationPhase {
	case PhaseActuation:
		return fmt.Sprintf("%s %s %s %s: %s",
			strings.ToLower(e.Relationship.String()),
			strings.ToLower(e.Strategy.String()),
			strings.ToLower(e.RelationPhase.String()),
			strings.ToLower(e.RelationActuationStatus.String()),
			e.Relation)
	case PhaseReconcile:
		return fmt.Sprintf("%s %s %s %s: %s",
			strings.ToLower(e.Relationship.String()),
			strings.ToLower(e.Strategy.String()),
			strings.ToLower(e.RelationPhase.String()),
			strings.ToLower(e.RelationReconcileStatus.String()),
			e.Relation)
	default:
		return fmt.Sprintf("invalid phase: %s", e.RelationPhase)
	}
}

func (e *DependencyPreventedActuationError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*DependencyPreventedActuationError)
	if !ok {
		return false
	}
	return e.Object == tErr.Object &&
		e.Strategy == tErr.Strategy &&
		e.Relationship == tErr.Relationship &&
		e.Relation == tErr.Relation &&
		e.RelationPhase == tErr.RelationPhase &&
		e.RelationActuationStatus == tErr.RelationActuationStatus &&
		e.RelationReconcileStatus == tErr.RelationReconcileStatus
}

type DependencyActuationMismatchError struct {
	Object       object.ObjMetadata
	Strategy     actuation.ActuationStrategy
	Relationship Relationship

	Relation         object.ObjMetadata
	RelationStrategy actuation.ActuationStrategy
}

func (e *DependencyActuationMismatchError) Error() string {
	return fmt.Sprintf("%s scheduled for %s: %s",
		strings.ToLower(e.Relationship.String()),
		strings.ToLower(e.RelationStrategy.String()),
		e.Relation)
}

func (e *DependencyActuationMismatchError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*DependencyActuationMismatchError)
	if !ok {
		return false
	}
	return e.Object == tErr.Object &&
		e.Strategy == tErr.Strategy &&
		e.Relationship == tErr.Relationship &&
		e.Relation == tErr.Relation &&
		e.RelationStrategy == tErr.RelationStrategy
}
