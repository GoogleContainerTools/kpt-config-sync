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

package filesystem

import (
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/reader"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigParser defines the minimum interface required for Reconciler to use a Parser to read
// configs from a filesystem.
type ConfigParser interface {
	Parse(filePaths reader.FilePaths) ([]ast.FileObject, status.MultiError)

	// ReadClusterRegistryResources returns the list of Clusters contained in the repo.
	ReadClusterRegistryResources(filePaths reader.FilePaths, sourceFormat SourceFormat) ([]ast.FileObject, status.MultiError)

	// ReadClusterNamesFromSelector returns the list of cluster names specified in
	// the `cluster-name-selector` annotation.
	ReadClusterNamesFromSelector(filePaths reader.FilePaths) ([]string, status.MultiError)
}

// AsCoreObjects converts a slice of FileObjects to a slice of client.Objects.
func AsCoreObjects(fos []ast.FileObject) []client.Object {
	result := make([]client.Object, len(fos))
	for i, fo := range fos {
		result[i] = fo.Unstructured
	}
	return result
}
