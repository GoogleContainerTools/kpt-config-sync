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
	"kpt.dev/configsync/pkg/importer/analyzer/transform"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/semantic"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Inheritance verifies that all syncable resources in an abstract namespace
// have a concrete Namespace as a descendant.
func Inheritance(tree *objects.Tree) status.MultiError {
	_, err := validateTreeNode(tree.Tree)
	return err
}

// validateTreeNode returns True if the give node is a Namespace or if any of
// its descendants are.
func validateTreeNode(node *ast.TreeNode) (bool, status.MultiError) {
	hasSyncableObjects := false
	var namespaces []client.Object
	for _, obj := range node.Objects {
		if obj.GetObjectKind().GroupVersionKind() == kinds.Namespace() {
			namespaces = append(namespaces, obj)
		} else if !transform.IsEphemeral(obj.GetObjectKind().GroupVersionKind()) {
			hasSyncableObjects = true
		}
	}

	if len(namespaces) > 1 {
		return true, status.MultipleSingletonsError(namespaces...)
	}
	if len(namespaces) == 1 {
		return true, nil
	}

	var errs status.MultiError
	foundDescendant := false
	for _, child := range node.Children {
		hasNamespaceDescendant, err := validateTreeNode(child)
		foundDescendant = foundDescendant || hasNamespaceDescendant
		errs = status.Append(errs, err)
	}
	if hasSyncableObjects {
		if len(node.Children) == 0 {
			errs = status.Append(errs, semantic.UnsyncableResourcesInLeaf(node))
		} else if !foundDescendant {
			errs = status.Append(errs, semantic.UnsyncableResourcesInNonLeaf(node))
		}
	}
	return foundDescendant, errs
}
