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

package semantic

import (
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/id"
	"kpt.dev/configsync/pkg/status"
)

// UnsyncableResourcesErrorCode is the error code for UnsyncableResourcesError
const UnsyncableResourcesErrorCode = "1044"

var unsyncableResourcesError = status.NewErrorBuilder(UnsyncableResourcesErrorCode)

// UnsyncableResourcesInLeaf reports that a leaf node has resources but is not a Namespace.
func UnsyncableResourcesInLeaf(dir id.TreeNode) status.Error {
	return unsyncableResourcesError.
		Sprintf("The directory %[2]q has configs, but is missing a %[1]s "+
			"config. All bottom level subdirectories MUST have a %[1]s config.", node.Namespace, dir.Name()).
		BuildWithPaths(dir)
}

// UnsyncableResourcesInNonLeaf reports that a node has resources and descendants, but none of its
// descendants are Namespaces.
func UnsyncableResourcesInNonLeaf(dir id.TreeNode) status.Error {
	return unsyncableResourcesError.
		Sprintf("The %[1]s directory named %[3]q has resources and "+
			"subdirectories, but none of its subdirectories are Namespaces. An %[1]s"+
			" MUST have at least one %[2]s subdirectory.", node.AbstractNamespace, node.Namespace, dir.Name()).
		BuildWithPaths(dir)
}
