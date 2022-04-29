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

package hydrate

import (
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

// ObjectNamespaces hydrates the given raw Objects by setting the metadata
// namespace field for objects that are located in a namespace directory but do
// not have their namespace specified yet.
func ObjectNamespaces(objs *objects.Raw) status.MultiError {
	namespaces := make(map[string]bool)
	for _, obj := range objs.Objects {
		if isValidHierarchicalNamespace(obj) {
			namespaces[obj.GetName()] = true
		}
	}
	for _, obj := range objs.Objects {
		if topLevelDir(obj) != repo.NamespacesDir {
			continue
		}
		// Namespaces and NamespaceSelectors are the only cluster-scoped objects
		// expected under the namespace/ directory, so we want to make sure we
		// don't accidentally assign them a namespace.
		gk := obj.GetObjectKind().GroupVersionKind().GroupKind()
		if gk == kinds.Namespace().GroupKind() || gk == kinds.NamespaceSelector().GroupKind() {
			continue
		}
		if obj.GetNamespace() == "" {
			dir := obj.Dir().Base()
			if namespaces[dir] {
				obj.SetNamespace(dir)
			}
		}
	}
	return nil
}

func isValidHierarchicalNamespace(obj ast.FileObject) bool {
	if obj.GetObjectKind().GroupVersionKind().GroupKind() != kinds.Namespace().GroupKind() {
		return false
	}
	if topLevelDir(obj) != repo.NamespacesDir {
		return false
	}
	return obj.GetName() == obj.Dir().Base()
}

func topLevelDir(obj ast.FileObject) string {
	sourcePath := obj.OSPath()
	return cmpath.RelativeSlash(sourcePath).Split()[0]
}
