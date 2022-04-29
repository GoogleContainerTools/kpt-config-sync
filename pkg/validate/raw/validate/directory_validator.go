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
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/metadata"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

// Directory verifies that the given FileObject is placed in a valid directory
// according to the following rules:
// - if the object is a Namespace, the directory must match its name
// - if the object has a metadata namespace, the directory must match it
func Directory(obj ast.FileObject) status.Error {
	if obj.GetObjectKind().GroupVersionKind() == kinds.Namespace() {
		return validateNamespaceDirectory(obj)
	}
	return validateObjectDirectory(obj)
}

func validateNamespaceDirectory(obj ast.FileObject) status.Error {
	// For Namespace "foo", we want to ensure it is located in:
	// namespaces/.../foo/
	// There is a separate top level directory validator that verifies it is under
	// "namespaces/..."  so here we just check for ".../foo/".
	expectedName := obj.Dir().Base()
	if expectedName == repo.NamespacesDir {
		return metadata.IllegalTopLevelNamespaceError(&obj)
	}
	if obj.GetName() != expectedName {
		return metadata.InvalidNamespaceNameError(&obj, expectedName)
	}
	return nil
}

func validateObjectDirectory(obj ast.FileObject) status.Error {
	if obj.GetNamespace() == "" {
		// We allow the metadata.namespace field to be left empty.
		return nil
	}
	// For an object in Namespace "foo", we want to ensure it is located in:
	// namespaces/.../foo/
	// There is a separate top level directory validator that verifies it is under
	// "namespaces/..."  so here we just check for ".../foo/".
	expectedNamespace := obj.Dir().Base()
	if obj.GetNamespace() != expectedNamespace {
		return metadata.IllegalMetadataNamespaceDeclarationError(&obj, expectedNamespace)
	}
	return nil
}
