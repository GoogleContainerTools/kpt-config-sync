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
	"errors"
	"testing"

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/semantic"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/objects"
)

func TestInheritance(t *testing.T) {
	testCases := []struct {
		name     string
		objs     *objects.Tree
		wantErrs status.MultiError
	}{
		{
			name: "empty tree",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
				},
			},
		},
		{
			name: "Namespace without resources",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.Namespace,
							Objects: []ast.FileObject{
								fake.Namespace("namespaces/hello"),
							},
						},
					},
				},
			},
		},
		{
			name: "Namespace with resource",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.Namespace,
							Objects: []ast.FileObject{
								fake.Namespace("namespaces/hello"),
								fake.RoleAtPath("namespaces/hello"),
							},
						},
					},
				},
			},
		},
		{
			name: "abstract namespace with ephemeral resource",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Objects: []ast.FileObject{
								fake.NamespaceSelector(),
							},
						},
					},
				},
			},
		},
		{
			name: "abstract namespace with resource and child namespace",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Objects: []ast.FileObject{
								fake.RoleAtPath("namespaces/hello"),
							},
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/world"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/hello/world"),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "abstract namespace with resource and descendant namespace",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Objects: []ast.FileObject{
								fake.RoleAtPath("namespaces/hello"),
							},
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/world"),
									Type:     node.AbstractNamespace,
									Children: []*ast.TreeNode{
										{
											Relative: cmpath.RelativeSlash("namespaces/hello/world/end"),
											Type:     node.Namespace,
											Objects: []ast.FileObject{
												fake.Namespace("namespaces/hello/world/end"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "abstract namespace with resource and no namespace child",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Objects: []ast.FileObject{
								fake.RoleAtPath("namespaces/hello"),
							},
						},
					},
				},
			},
			wantErrs: fake.Errors(semantic.UnsyncableResourcesErrorCode),
		},
		{
			name: "abstract namespace with resource and abstract child",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Objects: []ast.FileObject{
								fake.RoleAtPath("namespaces/hello"),
							},
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/world"),
									Type:     node.AbstractNamespace,
								},
							},
						},
					},
				},
			},
			wantErrs: fake.Errors(semantic.UnsyncableResourcesErrorCode),
		},
		{
			name: "abstract namespace with resource and duplicated descendant namespace",
			objs: &objects.Tree{
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Objects: []ast.FileObject{
								fake.RoleAtPath("namespaces/hello"),
							},
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/world"),
									Type:     node.AbstractNamespace,
									Children: []*ast.TreeNode{
										{
											Relative: cmpath.RelativeSlash("namespaces/hello/world/end"),
											Type:     node.Namespace,
											Objects: []ast.FileObject{
												fake.Namespace("namespaces/hello/world/end"),
												fake.Namespace("namespaces/hello/world/end"),
											},
										},
									},
								},
							},
						},
					},
				},
			},
			wantErrs: fake.Errors(status.MultipleSingletonsErrorCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := Inheritance(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got Inheritance() error %v, want %v", errs, tc.wantErrs)
			}
		})
	}
}
