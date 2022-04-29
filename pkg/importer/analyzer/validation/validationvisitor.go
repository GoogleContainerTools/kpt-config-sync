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

package validation

import (
	"strings"

	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/id"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalNamespaceSubdirectoryErrorCode is the error code for IllegalNamespaceSubdirectoryError
const IllegalNamespaceSubdirectoryErrorCode = "1003"

var illegalNamespaceSubdirectoryError = status.NewErrorBuilder(IllegalNamespaceSubdirectoryErrorCode)

// IllegalNamespaceSubdirectoryError represents an illegal child directory of a namespace directory.
func IllegalNamespaceSubdirectoryError(child, parent id.TreeNode) status.Error {
	// TODO: We don't really need the parent node since it can be inferred from the Child.
	return illegalNamespaceSubdirectoryError.Sprintf("A %[1]s directory MUST NOT have subdirectories. "+
		"Restructure %[4]q so that it does not have subdirectory %[2]q:\n\n"+
		"%[3]s",
		node.Namespace, child.Name(), id.PrintTreeNode(child), parent.Name()).BuildWithPaths(child, parent)
}

// IllegalAbstractNamespaceObjectKindErrorCode is the error code for IllegalAbstractNamespaceObjectKindError
const IllegalAbstractNamespaceObjectKindErrorCode = "1007"

var illegalAbstractNamespaceObjectKindError = status.NewErrorBuilder(IllegalAbstractNamespaceObjectKindErrorCode)

// IllegalAbstractNamespaceObjectKindError represents an illegal usage of a kind not allowed in abstract namespaces.
// TODO: Consolidate Illegal{X}ObjectKindErrors
func IllegalAbstractNamespaceObjectKindError(resource client.Object) status.Error {
	return illegalAbstractNamespaceObjectKindError.Sprintf(
		"Config %[3]q illegally declared in an %[1]s directory. "+
			"Move this config to a %[2]s directory:",
		strings.ToLower(string(node.AbstractNamespace)), strings.ToLower(string(node.Namespace)), resource.GetName()).
		BuildWithResources(resource)
}
