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

package status

import (
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ManagementConflictErrorCode is the error code for management conflict errors.
const ManagementConflictErrorCode = "1060"

// ManagementConflictErrorBuilder is the builder for management conflict errors.
var ManagementConflictErrorBuilder = NewErrorBuilder(ManagementConflictErrorCode)

// ManagementConflictError indicates that the passed resource is illegally
// declared in multiple repositories.
type ManagementConflictError interface {
	Error

	// ObjectID returns the ID of the object with the management conflict.
	ObjectID() core.ID
	// CurrentManager was the manager of the object on the cluster when the
	// conflict was detected. The object on the cluster may or may not have been
	// updated with the desired manager, after the error was detected, depending
	// on the adoption policy.
	CurrentManager() string
	// DesiredManager is the manager that detected the conflict. The object on
	// the cluster may or may not have been updated with the desired manager,
	// after the error was detected, depending on the adoption policy.
	DesiredManager() string
	// Invert returns a copy of the error with the current and desired managers
	// flipped. This is how the error would be reported by the other reconciler.
	Invert() ManagementConflictError
}

// ManagementConflictErrorWrap constructs a ManagementConflictError.
//
// The specific object is expected to be the current object from the cluster,
// with a "configsync.gke.io/manager" annotation specifying the current manager.
// The desired manager is expected to be the manager name of the reconciler that
// detected the conflict.
func ManagementConflictErrorWrap(obj client.Object, desiredManager string) ManagementConflictError {
	currentManager := obj.GetAnnotations()[metadata.ResourceManagerKey]
	return ManagementConflictErrorBuilder.
		Sprintf("The %q reconciler detected a management conflict with the %q reconciler. "+
			"Remove the object from one of the sources of truth so that the object is only managed by one reconciler.",
			desiredManager, currentManager).
		BuildWithConflictingManagers(obj, desiredManager, currentManager)
}

type managementConflictErrorImpl struct {
	underlying     Error
	object         client.Object
	desiredManager string
	currentManager string
}

var _ ManagementConflictError = &managementConflictErrorImpl{}

func (m *managementConflictErrorImpl) CurrentManager() string {
	return m.currentManager
}

func (m *managementConflictErrorImpl) DesiredManager() string {
	return m.desiredManager
}

func (m *managementConflictErrorImpl) Cause() error {
	return m.underlying.Cause()
}

func (m *managementConflictErrorImpl) Error() string {
	return format(m)
}

func (m *managementConflictErrorImpl) Errors() []Error {
	return []Error{m}
}

func (m *managementConflictErrorImpl) Invert() ManagementConflictError {
	obj := m.object.DeepCopyObject().(client.Object)
	core.SetAnnotation(obj, metadata.ResourceManagerKey, m.desiredManager)
	return ManagementConflictErrorWrap(obj, m.currentManager)
}

func (m *managementConflictErrorImpl) ToCME() v1.ConfigManagementError {
	cme := fromError(m)
	cme.ErrorResources = append(cme.ErrorResources, toErrorResource(m.object))
	return cme
}

func (m *managementConflictErrorImpl) ToCSE() v1beta1.ConfigSyncError {
	cse := cseFromError(m)
	cse.Resources = append(cse.Resources, toResourceRef(m.object))
	return cse
}

func (m *managementConflictErrorImpl) Code() string {
	return m.underlying.Code()
}

func (m *managementConflictErrorImpl) Body() string {
	return formatBody(m.underlying.Body(), "\n\n", formatResources(m.object))
}

func (m *managementConflictErrorImpl) Is(target error) bool {
	if target == nil {
		return false
	}
	return m.underlying.Is(target)
}

func (m *managementConflictErrorImpl) ObjectID() core.ID {
	return core.IDOf(m.object)
}
