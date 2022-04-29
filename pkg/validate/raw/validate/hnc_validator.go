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
	"strings"

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/hnc"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
)

// HasDepthSuffix returns true if the string ends with ".tree.hnc.x-k8s.io/depth".
func HasDepthSuffix(s string) bool {
	return strings.HasSuffix(s, metadata.DepthSuffix)
}

// HNCLabels verifies that the given object does not have any HNC depth labels.
func HNCLabels(obj ast.FileObject) status.Error {
	var errors []string
	for l := range obj.GetLabels() {
		if HasDepthSuffix(l) {
			errors = append(errors, l)
		}
	}
	if errors != nil {
		return hnc.IllegalDepthLabelError(&obj, errors)
	}
	return nil
}
