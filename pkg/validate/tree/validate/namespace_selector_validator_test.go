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

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/syntax"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/objects"
)

func TestNamespaceSelector(t *testing.T) {
	testCases := []struct {
		name     string
		objs     *objects.Tree
		wantErrs status.MultiError
	}{
		{
			name: "NamespaceSelector in abstract namespace",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"dev": fake.NamespaceSelectorAtPath("namespaces/sel.yaml",
						core.Name("dev")),
				},
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
			name: "NamespaceSelector in Namespace",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"dev": fake.NamespaceSelectorAtPath("namespaces/hello/sel.yaml",
						core.Name("dev")),
				},
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
			wantErrs: fake.Errors(syntax.IllegalKindInNamespacesErrorCode),
		},
		{
			name: "Object references ancestor NamespaceSelector",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"dev": fake.NamespaceSelectorAtPath("namespaces/hello/sel.yaml",
						core.Name("dev")),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/world"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/hello/world"),
										fake.RoleAtPath("namespaces/hello/world/role.yaml",
											core.Annotation(metadata.NamespaceSelectorAnnotationKey, "dev")),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "Object references non-ancestor NamespaceSelector",
			objs: &objects.Tree{
				NamespaceSelectors: map[string]ast.FileObject{
					"dev": fake.NamespaceSelectorAtPath("namespaces/goodbye/sel.yaml",
						core.Name("dev")),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/world"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/hello/world"),
										fake.RoleAtPath("namespaces/hello/world/role.yaml",
											core.Annotation(metadata.NamespaceSelectorAnnotationKey, "dev")),
									},
								},
							},
						},
						{
							Relative: cmpath.RelativeSlash("namespaces/goodbye"),
							Type:     node.AbstractNamespace,
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/goodbye/moon"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/goodbye/moon"),
										fake.RoleAtPath("namespaces/goodbye/moon/role.yaml"),
									},
								},
							},
						},
					},
				},
			},
			wantErrs: fake.Errors(selectors.ObjectHasUnknownSelectorCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := NamespaceSelector(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got NamespaceSelector() error %v, want %v", errs, tc.wantErrs)
			}
		})
	}
}
