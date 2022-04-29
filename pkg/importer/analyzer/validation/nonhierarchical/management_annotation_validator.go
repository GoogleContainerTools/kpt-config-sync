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

package nonhierarchical

import (
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ValidManagementAnnotation returns an Error if the user-specified management annotation is invalid.
func ValidManagementAnnotation(o ast.FileObject) status.Error {
	value, found := o.GetAnnotations()[metadata.ResourceManagementKey]
	if found && (value != metadata.ResourceManagementDisabled) {
		return IllegalManagementAnnotationError(&o, value)
	}
	return nil
}

// IllegalManagementAnnotationErrorCode is the error code for IllegalManagementAnnotationError.
const IllegalManagementAnnotationErrorCode = "1005"

var illegalManagementAnnotationError = status.NewErrorBuilder(IllegalManagementAnnotationErrorCode)

// IllegalManagementAnnotationError represents an illegal management annotation value.
// Error implements error.
func IllegalManagementAnnotationError(resource client.Object, value string) status.Error {
	return illegalManagementAnnotationError.
		Sprintf("Config has invalid management annotation %s=%s. If set, the value must be %q.",
			metadata.ResourceManagementKey, value, metadata.ResourceManagementDisabled).
		BuildWithResources(resource)
}
