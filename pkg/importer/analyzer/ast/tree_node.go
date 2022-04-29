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

package ast

import (
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/id"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

// TreeNode is analogous to a directory in the config hierarchy.
type TreeNode struct {
	// Path is the path this node has relative to a nomos Root.
	cmpath.Relative

	// The type of the HierarchyNode
	Type node.Type

	// Objects from the directory
	Objects []FileObject

	// children of the directory
	Children []*TreeNode
}

var _ id.TreeNode = &TreeNode{}

// PartialCopy makes an almost shallow copy of n.  An "almost shallow" copy of
// TreeNode make shallow copies of Children and members that are likely
// immutable.  A  deep copy is made of mutable members like Labels and
// Annotations.
func (n *TreeNode) PartialCopy() *TreeNode {
	nn := *n
	// Not sure if Selectors should be copied the same way.
	return &nn
}

// Name returns the name of the lowest-level directory in this node's path.
func (n *TreeNode) Name() string {
	return n.Base()
}

// Flatten returns the list of materialized FileObjects contained in this
// TreeNode. Specifically, it returns either
// 1) the list of Objects if this is a Namespace node, or
// 2) the concatenated list of all objects returned by calling Flatten on all of
// its children.
func (n *TreeNode) Flatten() []FileObject {
	switch n.Type {
	case node.Namespace:
		return n.flattenNamespace()
	case node.AbstractNamespace:
		return n.flattenAbstractNamespace()
	default:
		panic(status.InternalErrorf("invalid node type: %q", string(n.Type)))
	}
}

func (n *TreeNode) flattenNamespace() []FileObject {
	var result []FileObject
	for _, o := range n.Objects {
		if o.GetObjectKind().GroupVersionKind() != kinds.Namespace() {
			o.SetNamespace(n.Name())
		}
		result = append(result, o)
	}
	return result
}

func (n *TreeNode) flattenAbstractNamespace() []FileObject {
	var result []FileObject

	for _, o := range n.Objects {
		if o.GetObjectKind().GroupVersionKind() == kinds.NamespaceSelector() {
			result = append(result, o)
		}
	}
	for _, child := range n.Children {
		result = append(result, child.Flatten()...)
	}

	return result
}
