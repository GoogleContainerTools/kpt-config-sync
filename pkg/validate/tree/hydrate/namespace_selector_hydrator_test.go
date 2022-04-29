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
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/objects"
)

func TestNamespaceSelectors(t *testing.T) {
	namespaceSelectorObject := fake.NamespaceSelectorObject(core.Name("sre"))
	namespaceSelectorObject.Spec.Selector.MatchLabels = map[string]string{
		"sre-support": "true",
	}

	testCases := []struct {
		name string
		objs *objects.Tree
		want *objects.Tree
	}{
		{
			name: "Object without selector is kept",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend",
											core.Label("sre-support", "false")),
										fake.RoleAtPath("namespaces/foo/role.yaml",
											core.Namespace("frontend")),
									},
								},
							},
						},
					},
				},
			},
			want: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend",
											core.Label("sre-support", "false")),
										fake.RoleAtPath("namespaces/foo/role.yaml",
											core.Namespace("frontend")),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Object outside selector dir is kept",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend",
											core.Label("sre-support", "false")),
									},
								},
							},
						},
						{
							Relative: cmpath.RelativeSlash("namespaces/bar"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/bar/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/bar/frontend"),
										fake.RoleAtPath("namespaces/bar/role.yaml",
											core.Namespace("bar")),
									},
								},
							},
						},
					},
				},
			},
			want: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend",
											core.Label("sre-support", "false")),
									},
								},
							},
						},
						{
							Relative: cmpath.RelativeSlash("namespaces/bar"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/bar/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/bar/frontend"),
										fake.RoleAtPath("namespaces/bar/role.yaml",
											core.Namespace("bar")),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Object and Namespace with labels is kept",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend",
											core.Label("sre-support", "true")),
										fake.RoleAtPath("namespaces/foo/role.yaml",
											core.Namespace("frontend"),
											core.Annotation(metadata.NamespaceSelectorAnnotationKey, "sre")),
									},
								},
							},
						},
					},
				},
			},
			want: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend",
											core.Label("sre-support", "true")),
										fake.RoleAtPath("namespaces/foo/role.yaml",
											core.Namespace("frontend"),
											core.Annotation(metadata.NamespaceSelectorAnnotationKey, "sre")),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Object with selector and Namespace without labels is not kept",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend"),
										fake.RoleAtPath("namespaces/foo/role.yaml",
											core.Namespace("frontend"),
											core.Annotation(metadata.NamespaceSelectorAnnotationKey, "sre")),
									},
								},
							},
						},
					},
				},
			},
			want: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"sre": fake.FileObject(namespaceSelectorObject, "namespaces/foo/selector.yaml"),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/foo"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/foo/frontend"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/foo/frontend"),
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := NamespaceSelectors(tc.objs)
			if errs != nil {
				t.Errorf("Got NamespaceSelectors() error %v, want nil", errs)
			}
			if tc.want != nil {
				if diff := cmp.Diff(tc.want, tc.objs, ast.CompareFileObject); diff != "" {
					t.Error(diff)
				}
			}
		})
	}
}
