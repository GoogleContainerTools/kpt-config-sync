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

package objects

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/ast/node"
	"kpt.dev/configsync/pkg/importer/analyzer/validation"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestBuildTree(t *testing.T) {
	testCases := []struct {
		name     string
		from     *Scoped
		want     *Tree
		wantErrs status.MultiError
	}{
		{
			name: "almost-empty tree",
			from: &Scoped{
				Cluster: []ast.FileObject{
					fake.Repo(),
				},
			},
			want: &Tree{
				Repo:               fake.Repo(),
				NamespaceSelectors: map[string]ast.FileObject{},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash(""),
					Type:     node.AbstractNamespace,
				},
			},
		},
		{
			name: "populated tree",
			from: &Scoped{
				Cluster: []ast.FileObject{
					fake.Repo(),
					fake.HierarchyConfig(),
					fake.ClusterRole(core.Name("hello-reader")),
					fake.Namespace("namespaces/hello/world"),
					fake.Namespace("namespaces/hello/moon"),
					fake.NamespaceSelectorAtPath("namespaces/selector.yaml"),
				},
				Namespace: []ast.FileObject{
					fake.RoleAtPath("namespaces/hello/role.yaml", core.Name("writer")),
				},
				Unknown: []ast.FileObject{
					fake.AnvilAtPath("namespaces/hello/world/anvil.yaml"),
				},
			},
			want: &Tree{
				Repo: fake.Repo(),
				HierarchyConfigs: []ast.FileObject{
					fake.HierarchyConfig(),
				},
				NamespaceSelectors: map[string]ast.FileObject{
					"default-name": fake.NamespaceSelectorAtPath("namespaces/selector.yaml"),
				},
				Cluster: []ast.FileObject{
					fake.ClusterRole(core.Name("hello-reader")),
				},
				Tree: &ast.TreeNode{
					Relative: cmpath.RelativeSlash("namespaces"),
					Type:     node.AbstractNamespace,
					Children: []*ast.TreeNode{
						{
							Relative: cmpath.RelativeSlash("namespaces/hello"),
							Type:     node.AbstractNamespace,
							Objects: []ast.FileObject{
								fake.RoleAtPath("namespaces/hello/role.yaml", core.Name("writer")),
							},
							Children: []*ast.TreeNode{
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/moon"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/hello/moon"),
									},
								},
								{
									Relative: cmpath.RelativeSlash("namespaces/hello/world"),
									Type:     node.Namespace,
									Objects: []ast.FileObject{
										fake.Namespace("namespaces/hello/world"),
										fake.AnvilAtPath("namespaces/hello/world/anvil.yaml"),
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "cluster-scoped resource in wrong directory",
			from: &Scoped{
				Cluster: []ast.FileObject{
					fake.Repo(),
					fake.ClusterRoleAtPath("namespaces/hello/cr.yaml", core.Name("hello-reader")),
					fake.Namespace("namespaces/hello"),
				},
				Namespace: []ast.FileObject{
					fake.RoleAtPath("namespaces/hello/role.yaml", core.Name("writer")),
				},
			},
			want:     nil,
			wantErrs: status.Append(nil, validation.ShouldBeInClusterError(fake.ClusterRole())),
		},
		{
			name: "namespace-scoped resource in wrong directory",
			from: &Scoped{
				Cluster: []ast.FileObject{
					fake.Repo(),
					fake.Namespace("namespaces/hello"),
				},
				Namespace: []ast.FileObject{
					fake.RoleAtPath("cluster/role.yaml", core.Name("writer")),
				},
			},
			want:     nil,
			wantErrs: status.Append(nil, validation.ShouldBeInNamespacesError(fake.Role())),
		},
		{
			name: "system resource in wrong directory",
			from: &Scoped{
				Cluster: []ast.FileObject{
					fake.Repo(),
					fake.HierarchyConfigAtPath("cluster/hc.yaml"),
					fake.Namespace("namespaces/hello"),
				},
			},
			want:     nil,
			wantErrs: status.Append(nil, validation.ShouldBeInSystemError(fake.HierarchyConfig())),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, errs := BuildTree(tc.from)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("Got BuildTree() error %v, want %v", errs, tc.wantErrs)
			}
			if diff := cmp.Diff(tc.want, got, ast.CompareFileObject); diff != "" {
				t.Error(diff)
			}
		})
	}
}
