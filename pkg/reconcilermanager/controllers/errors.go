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

package controllers

import (
	"fmt"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// OperationCreate is the create operation
	OperationCreate = Operation("create")
	// OperationUpdate is the update operation
	OperationUpdate = Operation("update")
	// OperationPatch is the patch operation
	OperationPatch = Operation("patch")
	// OperationDelete is the delete operation
	OperationDelete = Operation("delete")
	// OperationGet is the get operation
	OperationGet = Operation("get")
	// OperationList is the list operation
	OperationList = Operation("list")
	// OperationWatch is the watch operation
	OperationWatch = Operation("watch")
)

// Operation performed on a Kubernetes resource or object
type Operation string

// ObjectOperationError is an error from the reconciler-manager regarding
// failure to perform an operation on a managed Kubernetes resource or resource
// object.
type ObjectOperationError struct {
	// ID of the managed object
	ID core.ID
	// Operation attempted on the managed object
	Operation Operation
	// Cause of the operation failure
	Cause error
}

// Error returns the error string
func (ooe *ObjectOperationError) Error() string {
	return fmt.Sprintf("%s (%s) %s failed: %v",
		ooe.ID.Kind, ooe.ID.ObjectKey, ooe.Operation, ooe.Cause)
}

// Unwrap returns the cause of the error, to allow type checking with errors.Is
// and errors.As.
func (ooe *ObjectOperationError) Unwrap() error {
	return ooe.Cause
}

// NewObjectOperationError constructs a new ObjectOperationError
func NewObjectOperationError(err error, obj client.Object, op Operation) *ObjectOperationError {
	id := core.IDOf(obj)
	if id.GroupKind.Empty() {
		gvk, lookupErr := kinds.Lookup(obj, core.Scheme)
		if lookupErr != nil {
			// Invalid or unregistered resource type - err probably already includes the same message
			// Fake the kind with the struct type
			id.Kind = fmt.Sprintf("%T", obj)
		} else {
			id.GroupKind = gvk.GroupKind()
		}
	}
	return &ObjectOperationError{
		ID:        id,
		Operation: op,
		Cause:     err,
	}
}

// NewObjectOperationErrorWithKey constructs a new ObjectOperationError and
// overrides the Object's key with the specified ObjectKey.
// This is useful if you don't know whether the Object's key will be populated.
func NewObjectOperationErrorWithKey(err error, obj client.Object, op Operation, objKey client.ObjectKey) *ObjectOperationError {
	ooe := NewObjectOperationError(err, obj, op)
	ooe.ID.ObjectKey = objKey
	return ooe
}

// NewObjectOperationErrorWithID constructs a new ObjectOperationError with a
// specific ID.
func NewObjectOperationErrorWithID(err error, id core.ID, op Operation) *ObjectOperationError {
	return &ObjectOperationError{
		ID:        id,
		Operation: op,
		Cause:     err,
	}
}

// NewObjectOperationErrorForList constructs a new ObjectOperationError for a
// list of objects with the same resource.
func NewObjectOperationErrorForList(err error, objList client.ObjectList, op Operation) *ObjectOperationError {
	id := core.ID{}
	listGVK, lookupErr := kinds.Lookup(objList, core.Scheme)
	if lookupErr != nil {
		// Invalid or unregistered resource type - err probably already includes the same message
		// Fake the kind with the struct type
		id.Kind = fmt.Sprintf("%T", objList)
	} else {
		id.GroupKind = kinds.ItemGVKForListGVK(listGVK).GroupKind()
	}
	return &ObjectOperationError{
		ID:        id,
		Operation: op,
		Cause:     err,
	}
}

// NewObjectOperationErrorForListWithNamespace constructs a new
// ObjectOperationError for a list of objects with the same resource and
// namespace.
func NewObjectOperationErrorForListWithNamespace(err error, objList client.ObjectList, op Operation, namespace string) *ObjectOperationError {
	ooe := NewObjectOperationErrorForList(err, objList, op)
	ooe.ID.ObjectKey = client.ObjectKey{Namespace: namespace}
	return ooe
}

// ObjectReconcileError is an error from the status of a managed resource object
type ObjectReconcileError struct {
	// ID of the managed object
	ID core.ID
	// Status of the managed object
	Status kstatus.Status
	// Cause of the operation failure
	Cause error
}

// Error returns the error string
func (oripe *ObjectReconcileError) Error() string {
	return fmt.Sprintf("%s (%s) %s: %v",
		oripe.ID.Kind, oripe.ID.ObjectKey, oripe.Status, oripe.Cause)
}

// Unwrap returns the cause of the error, to allow type checking with errors.Is
// and errors.As.
func (oripe *ObjectReconcileError) Unwrap() error {
	return oripe.Cause
}

// NewObjectReconcileError constructs a new ObjectReconcileError
func NewObjectReconcileError(err error, obj client.Object, status kstatus.Status) *ObjectReconcileError {
	id := core.IDOf(obj)
	if id.GroupKind.Empty() {
		gvk, lookupErr := kinds.Lookup(obj, core.Scheme)
		if lookupErr != nil {
			// Invalid or unregistered resource type - err probably already includes the same message
			// Fake the kind with the struct type
			id.Kind = fmt.Sprintf("%T", obj)
		} else {
			id.GroupKind = gvk.GroupKind()
		}
	}
	return &ObjectReconcileError{
		ID:     id,
		Status: status,
		Cause:  err,
	}
}

// NewObjectReconcileErrorWithID constructs a new ObjectReconcileError with the
// specified ID.
func NewObjectReconcileErrorWithID(err error, id core.ID, status kstatus.Status) *ObjectReconcileError {
	return &ObjectReconcileError{
		ID:     id,
		Status: status,
		Cause:  err,
	}
}

// NoRetryError is an error that should not immediately trigger a reconcile retry.
type NoRetryError struct {
	Cause error
}

// NewNoRetryError constructs a new NewNoRetryError
func NewNoRetryError(cause error) *NoRetryError {
	return &NoRetryError{Cause: cause}
}

// Error returns the error message
func (n *NoRetryError) Error() string {
	return fmt.Sprintf("no retry: %v", n.Cause)
}

// Unwrap returns the cause of this NoRetryError
func (n *NoRetryError) Unwrap() error {
	return n.Cause
}
