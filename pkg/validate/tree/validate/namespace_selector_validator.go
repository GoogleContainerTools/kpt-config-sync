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
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/syntax"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceSelector performs a second round of verification on namespace
// selector annotations. This one verifies that legacy NamespaceSelectors are
// only declared in abstract namespace and that objects only reference legacy
// NamespaceSelectors that are in an ancestor abstract namespace.
func NamespaceSelector(tree *objects.Tree) status.MultiError {
	return validateSelectorsInNode(tree.Tree, tree.NamespaceSelectors)
}

func validateSelectorsInNode(node *ast.TreeNode, nsSelectors map[string]ast.FileObject) status.MultiError {
	err := validateNamespaceSelectors(node.Objects, nsSelectors)

	for _, c := range node.Children {
		err = status.Append(err, validateSelectorsInNode(c, nsSelectors))
	}
	return err
}

// validateNamespaceSelectors returns an error if the given objects contain both
// a Namespace and one or more NamespaceSelectors.
func validateNamespaceSelectors(objs []ast.FileObject, nsSelectors map[string]ast.FileObject) status.MultiError {
	var errs status.MultiError
	var namespaceDir string
	for _, obj := range objs {
		switch obj.GetObjectKind().GroupVersionKind() {
		case kinds.Namespace():
			namespaceDir = obj.Dir().SlashPath()
		default:
			name, hasSelector := obj.GetAnnotations()[metadata.NamespaceSelectorAnnotationKey]
			if hasSelector {
				selector, known := nsSelectors[name]
				if !known {
					errs = status.Append(errs, selectors.ObjectHasUnknownNamespaceSelector(obj, name))
				} else if !strings.HasPrefix(obj.SlashPath(), selector.Dir().SlashPath()) {
					// The NamespaceSelector is not in a parent directory of this object, so error.
					errs = status.Append(errs, selectors.ObjectNotInNamespaceSelectorSubdirectory(obj, selector))
				}
			}
		}
	}
	if namespaceDir != "" {
		selectorsInNode := selectorsInNamespaceDir(namespaceDir, nsSelectors)
		if len(selectorsInNode) > 0 {
			errs = status.Append(errs, syntax.IllegalKindInNamespacesError(selectorsInNode...))
		}
	}
	return errs
}

func selectorsInNamespaceDir(dir string, nsSelectors map[string]ast.FileObject) []client.Object {
	var result []client.Object
	for _, obj := range nsSelectors {
		if obj.Dir().SlashPath() == dir {
			result = append(result, obj)
		}
	}
	return result
}
