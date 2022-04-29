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

package metadata

import (
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalTopLevelNamespaceErrorCode is the error code for IllegalTopLevelNamespaceError
const IllegalTopLevelNamespaceErrorCode = "1019"

var illegalTopLevelNamespaceError = status.NewErrorBuilder(IllegalTopLevelNamespaceErrorCode)

// IllegalTopLevelNamespaceError reports that there may not be a Namespace declared directly in namespaces/
// Error implements error
func IllegalTopLevelNamespaceError(resource client.Object) status.Error {
	return illegalTopLevelNamespaceError.
		Sprintf("%[2]ss MUST be declared in subdirectories of '%[1]s/'. Create a subdirectory for the following %[2]s configs:",
			repo.NamespacesDir, node.Namespace).
		BuildWithResources(resource)
}

// InvalidNamespaceNameErrorCode is the error code for InvalidNamespaceNameError
const InvalidNamespaceNameErrorCode = "1020"

var invalidNamespaceNameErrorBuilder = status.NewErrorBuilder(InvalidNamespaceNameErrorCode)

// InvalidNamespaceNameError reports that a Namespace has an invalid name.
func InvalidNamespaceNameError(resource client.Object, expected string) status.Error {
	return invalidNamespaceNameErrorBuilder.
		Sprintf("A %[1]s MUST declare `metadata.name` that matches the name of its directory.\n\n"+
			"expected `metadata.name`: %[2]s",
			node.Namespace, expected).
		BuildWithResources(resource)
}
