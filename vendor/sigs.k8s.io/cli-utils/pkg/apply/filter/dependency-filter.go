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

// Filter returns true if the specified object should be skipped because at
// least one of its dependencies is Not Found or Not Reconciled.
func (dnrf DependencyFilter) Filter(obj *unstructured.Unstructured) (bool, string, error) {
	id := object.UnstructuredToObjMetadata(obj)

	switch dnrf.ActuationStrategy {
	case actuation.ActuationStrategyApply:
		// For apply, check dependencies (outgoing)
		for _, depID := range dnrf.TaskContext.Graph().Dependencies(id) {
			skip, reason, err := dnrf.filterByRelationStatus(depID, RelationshipDependency)
			if err != nil {
				return false, "", err
			}
			if skip {
				return skip, reason, nil
			}
		}
	case actuation.ActuationStrategyDelete:
		// For delete, check dependents (incoming)
		for _, depID := range dnrf.TaskContext.Graph().Dependents(id) {
			skip, reason, err := dnrf.filterByRelationStatus(depID, RelationshipDependent)
			if err != nil {
				return false, "", err
			}
			if skip {
				return skip, reason, nil
			}
		}
	default:
		panic(fmt.Sprintf("invalid filter strategy: %q", dnrf.ActuationStrategy))
	}
	return false, "", nil
}

func (dnrf DependencyFilter) filterByRelationStatus(id object.ObjMetadata, relationship Relationship) (bool, string, error) {
	// Dependency on an invalid object is considered an invalid dependency, making both objects invalid.
	// For applies: don't prematurely apply something that depends on something that hasn't been applied (because invalid).
	// For deletes: don't prematurely delete something that depends on something that hasn't been deleted (because invalid).
	// These can't be caught be subsequent checks, because invalid objects aren't in the inventory.
	if dnrf.TaskContext.IsInvalidObject(id) {
		// Skip!
		return true, fmt.Sprintf("%s invalid: %q",
			strings.ToLower(relationship.String()),
			id), nil
	}

	status, found := dnrf.TaskContext.InventoryManager().ObjectStatus(id)
	if !found {
		// Status is registered during planning.
		// So if status is not found, the object is external (NYI) or invalid.
		return false, "", fmt.Errorf("unknown %s actuation strategy: %v",
			strings.ToLower(relationship.String()), id)
	}

	// Dependencies must have the same actuation strategy.
	// If there is a mismatch, skip both.
	if status.Strategy != dnrf.ActuationStrategy {
		return true, fmt.Sprintf("%s skipped because %s is scheduled for %s: %q",
			strings.ToLower(dnrf.ActuationStrategy.String()),
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Strategy.String()),
			id), nil
	}

	switch status.Actuation {
	case actuation.ActuationPending:
		// If actuation is still pending, dependency sorting is probably broken.
		return false, "", fmt.Errorf("premature %s: %s %s actuation %s: %q",
			strings.ToLower(dnrf.ActuationStrategy.String()),
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Strategy.String()),
			strings.ToLower(status.Actuation.String()),
			id)
	case actuation.ActuationSkipped, actuation.ActuationFailed:
		// Skip!
		return true, fmt.Sprintf("%s %s actuation %s: %q",
			strings.ToLower(relationship.String()),
			strings.ToLower(dnrf.ActuationStrategy.String()),
			strings.ToLower(status.Actuation.String()),
			id), nil
	case actuation.ActuationSucceeded:
		// Don't skip!
	default:
		return false, "", fmt.Errorf("invalid %s apply status %q: %q",
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Actuation.String()),
			id)
	}

	// DryRun skips WaitTasks, so reconcile status can be ignored
	if dnrf.DryRunStrategy.ClientOrServerDryRun() {
		// Don't skip!
		return false, "", nil
	}

	switch status.Reconcile {
	case actuation.ReconcilePending:
		// If reconcile is still pending, dependency sorting is probably broken.
		return false, "", fmt.Errorf("premature %s: %s %s reconcile %s: %q",
			strings.ToLower(dnrf.ActuationStrategy.String()),
			strings.ToLower(relationship.String()),
			strings.ToLower(status.Strategy.String()),
			strings.ToLower(status.Reconcile.String()),
			id)
	case actuation.ReconcileSkipped, actuation.ReconcileFailed, actuation.ReconcileTimeout:
		// Skip!
		return true, fmt.Sprintf("%s %s reconcile %s: %q",
			strings.ToLower(relationship.String()),
			strings.ToLower(dnrf.ActuationStrategy.String()),
			strings.ToLower(status.Reconcile.String()),
			id), nil
	case actuation.ReconcileSucceeded:
		// Don't skip!
	default:
		return false, "", fmt.Errorf("invalid dependency reconcile status %q: %q",
			strings.ToLower(status.Reconcile.String()), id)
	}

	// Don't skip!
	return false, "", nil
}
