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
	"kpt.dev/configsync/pkg/importer/analyzer/validation/metadata"
	csmetadata "kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
)

// IsInvalidLabel returns true if the label cannot be declared by users.
func IsInvalidLabel(k string) bool {
	return csmetadata.HasConfigSyncPrefix(k)
}

// Labels verifies that the given object does not have any invalid labels.
func Labels(obj ast.FileObject) status.Error {
	var invalid []string
	for l := range obj.GetLabels() {
		if IsInvalidLabel(l) {
			invalid = append(invalid, l)
		}
	}
	if len(invalid) > 0 {
		return metadata.IllegalLabelDefinitionError(&obj, invalid)
	}
	return nil
}
