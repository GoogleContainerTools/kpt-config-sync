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
	"fmt"

	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
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
	// ConflictingManager returns the annotation value of the other conflicting manager.
	ConflictingManager() string
	// CurrentManagerError returns the error that will be surfaced to the current manager.
	CurrentManagerError() ManagementConflictError
	// ConflictingManagerError returns the error that will be surfaced to the other conflicting manager.
	ConflictingManagerError() ManagementConflictError
	Error
}

func currentErrorMsg(newManager, currentManager string) string {
	return fmt.Sprintf("The %q reconciler cannot manage resources declared in another repository. "+
		"Remove the declaration for this resource from either the current repository, or the repository managed by %q.",
		newManager, currentManager)
}

// ManagementConflictErrorWrap returns the ManagementConflictError.
func ManagementConflictErrorWrap(resource client.Object, newManager string) ManagementConflictError {
	currentManager := resource.GetAnnotations()[metadata.ResourceManagerKey]
	return ManagementConflictErrorBuilder.
		Sprint(currentErrorMsg(newManager, currentManager)).
		BuildWithConflictingManagers(resource, newManager, currentManager)
}

type managementConflictErrorImpl struct {
	underlying Error
	resource   client.Object
	// newManager refers to the manager annotation for the current remediator/reconciler.
	newManager string
	// currentManager is the manager annotation in the actual resource. It is also known as conflictingManager.
	currentManager string
}

var _ ManagementConflictError = managementConflictErrorImpl{}

func (m managementConflictErrorImpl) ConflictingManager() string {
	return m.currentManager
}

func (m managementConflictErrorImpl) CurrentManagerError() ManagementConflictError {
	return ManagementConflictErrorBuilder.
		Sprint(currentErrorMsg(m.newManager, m.newManager)).
		BuildWithConflictingManagers(m.resource, m.newManager, m.currentManager)
}

func (m managementConflictErrorImpl) ConflictingManagerError() ManagementConflictError {
	return ManagementConflictErrorBuilder.
		Sprintf("The %q reconciler detects a management conflict for a resource declared in another repository. "+
			"Remove the declaration for this resource from either the current repository, or the repository managed by %q.",
			m.newManager, m.newManager).
		BuildWithConflictingManagers(m.resource, m.newManager, m.currentManager)
}

func (m managementConflictErrorImpl) Cause() error {
	return m.underlying.Cause()
}

func (m managementConflictErrorImpl) Error() string {
	return format(m)
}

func (m managementConflictErrorImpl) Errors() []Error {
	return []Error{m}
}

func (m managementConflictErrorImpl) ToCME() v1.ConfigManagementError {
	cme := fromError(m)
	cme.ErrorResources = append(cme.ErrorResources, toErrorResource(m.resource))
	return cme
}

func (m managementConflictErrorImpl) ToCSE() v1beta1.ConfigSyncError {
	cse := cseFromError(m)
	cse.Resources = append(cse.Resources, toResourceRef(m.resource))
	return cse
}

func (m managementConflictErrorImpl) Code() string {
	return m.underlying.Code()
}

func (m managementConflictErrorImpl) Body() string {
	return formatBody(m.underlying.Body(), "\n\n", formatResources(m.resource))
}

func (m managementConflictErrorImpl) Is(target error) bool {
	return m.underlying.Is(target)
}
