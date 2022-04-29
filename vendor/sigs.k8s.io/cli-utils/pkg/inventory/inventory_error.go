// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Errors when applying inventory object templates.

package inventory

import "sigs.k8s.io/cli-utils/pkg/object"

const noInventoryErrorStr = `Package uninitialized. Please run "init" command.

The package needs to be initialized to generate the template
which will store state for resource sets. This state is
necessary to perform functionality such as deleting an entire
package or automatically deleting omitted resources (pruning).
`

const multipleInventoryErrorStr = `Package has multiple inventory object templates.

The package should have one and only one inventory object template.
`

const inventoryNamespaceInSet = `Inventory use namespace defined in package.

The inventory cannot use a namespace that is defined in the package.
`

type NoInventoryObjError struct{}

func (g NoInventoryObjError) Error() string {
	return noInventoryErrorStr
}

type MultipleInventoryObjError struct {
	InventoryObjectTemplates object.UnstructuredSet
}

func (g MultipleInventoryObjError) Error() string {
	return multipleInventoryErrorStr
}

//nolint:revive // redundant name in exported error ok
type InventoryNamespaceInSet struct {
	Namespace string
}

func (g InventoryNamespaceInSet) Error() string {
	return inventoryNamespaceInSet
}

//nolint:revive // redundant name in exported error ok
type InventoryOverlapError struct {
	err error
}

func (e *InventoryOverlapError) Error() string {
	return e.err.Error()
}

func NewInventoryOverlapError(err error) *InventoryOverlapError {
	return &InventoryOverlapError{err: err}
}

type NeedAdoptionError struct {
	err error
}

func (e *NeedAdoptionError) Error() string {
	return e.err.Error()
}

func NewNeedAdoptionError(err error) *NeedAdoptionError {
	return &NeedAdoptionError{err: err}
}
