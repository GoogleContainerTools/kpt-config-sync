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

package validate

import (
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
)

// ManagementAnnotation returns an Error if the user-specified management annotation is invalid.
func ManagementAnnotation(obj ast.FileObject) status.Error {
	value, found := obj.GetAnnotations()[metadata.ResourceManagementKey]
	if found && (value != metadata.ResourceManagementDisabled) {
		return nonhierarchical.IllegalManagementAnnotationError(obj, value)
	}
	return nil
}
