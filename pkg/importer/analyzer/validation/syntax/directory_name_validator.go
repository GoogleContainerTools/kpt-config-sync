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

package syntax

import (
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
)

// InvalidDirectoryNameErrorCode is the error code for InvalidDirectoryNameError
const InvalidDirectoryNameErrorCode = "1028"

var invalidDirectoryNameError = status.NewErrorBuilder(InvalidDirectoryNameErrorCode)

// ReservedDirectoryNameError represents an illegal usage of a reserved name.
func ReservedDirectoryNameError(dir cmpath.Relative) status.Error {
	// TODO: Consider moving to Namespace validation instead.
	//  Strictly speaking, having a directory named "config-management-system" doesn't necessarily mean there are
	//  any resources declared in that Namespace. That would make this error message clearer.
	return invalidDirectoryNameError.
		Sprintf("%s repositories MUST NOT declare configs in the %s Namespace. Rename or remove the %q directory.",
			configmanagement.ProductName, configmanagement.ControllerNamespace, dir.Base()).
		BuildWithPaths(dir)
}

// InvalidDirectoryNameError represents an illegal usage of a reserved name.
func InvalidDirectoryNameError(dir cmpath.Relative) status.Error {
	return invalidDirectoryNameError.
		Sprintf(`Directory names MUST be valid Kubernetes Namespace names and must not be "config-management-system". Rename %q so that it:
1. has a length of 63 characters or fewer;
2. consists only of lowercase letters (a-z), digits (0-9), and hyphen '-';
3. begins and ends with a lowercase letter or digit`, dir.Base()).
		BuildWithPaths(dir)
}
