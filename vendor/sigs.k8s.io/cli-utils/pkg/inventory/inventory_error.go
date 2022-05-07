// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Errors when applying inventory object templates.

package inventory

import (
	"fmt"

	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/object"
)

const noInventoryErrorStr = `Package uninitialized. Please run "init" command.

The package needs to be initialized to generate the template
which will store state for resource sets. This state is
necessary to perform functionality such as deleting an entire
package or automatically deleting omitted resources (pruning).
`

const multipleInventoryErrorStr = `Package has multiple inventory object templates.

The package should have one and only one inventory object template.
`

type NoInventoryObjError struct{}

func (e *NoInventoryObjError) Error() string {
	return noInventoryErrorStr
}

// Is returns true if the specified error is equal to this error.
// Use errors.Is(error) to recursively check if an error wraps this error.
func (e *NoInventoryObjError) Is(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*NoInventoryObjError)
	return ok
}

type MultipleInventoryObjError struct {
	InventoryObjectTemplates object.UnstructuredSet
}

func (e *MultipleInventoryObjError) Error() string {
	return multipleInventoryErrorStr
}

// Is returns true if the specified error is equal to this error.
// Use errors.Is(error) to recursively check if an error wraps this error.
func (e *MultipleInventoryObjError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*MultipleInventoryObjError)
	if !ok {
		return false
	}
	return e.InventoryObjectTemplates.Equal(tErr.InventoryObjectTemplates)
}

type PolicyPreventedActuationError struct {
	Strategy actuation.ActuationStrategy
	Policy   Policy
	Status   IDMatchStatus
}

func (e *PolicyPreventedActuationError) Error() string {
	return fmt.Sprintf("inventory policy prevented actuation (strategy: %s, status: %s, policy: %s)",
		e.Strategy, e.Status, e.Policy)
}

// Is returns true if the specified error is equal to this error.
// Use errors.Is(error) to recursively check if an error wraps this error.
func (e *PolicyPreventedActuationError) Is(err error) bool {
	if err == nil {
		return false
	}
	tErr, ok := err.(*PolicyPreventedActuationError)
	if !ok {
		return false
	}
	return e.Strategy == tErr.Strategy &&
		e.Policy == tErr.Policy &&
		e.Status == tErr.Status
}
