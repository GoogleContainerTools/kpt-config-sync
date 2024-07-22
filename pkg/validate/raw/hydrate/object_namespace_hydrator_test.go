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
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

func TestObjectNamespaces(t *testing.T) {
	testCases := []struct {
		name string
		objs *fileobjects.Raw
		want *fileobjects.Raw
	}{
		{
			name: "Set namespace on object in namespace directory",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader"),
						core.Namespace("foo")),
					k8sobjects.RoleBindingAtPath("namespaces/foo/rb.yaml",
						core.Name("reader-binding")),
				},
			},
			want: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader"),
						core.Namespace("foo")),
					k8sobjects.RoleBindingAtPath("namespaces/foo/rb.yaml",
						core.Name("reader-binding"),
						core.Namespace("foo")),
				},
			},
		},
		{
			// In this case, we have a validator that will catch this error and report
			// it later. So the main thing here is to make sure that we don't
			// accidentally change an incorrect namespace to the correct namespace.
			name: "Ignore object with incorrect namespace already set",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader"),
						core.Namespace("bar")),
				},
			},
			want: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader"),
						core.Namespace("bar")),
				},
			},
		},
		{
			name: "Ignore object in abstract namespace directory",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo/bar"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader")),
				},
			},
			want: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo/bar"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader")),
				},
			},
		},
		{
			name: "Ignore objects in non-namespaced directories",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.ClusterAtPath("clusterregistry/cluster.yaml"),
					k8sobjects.ClusterRoleAtPath("cluster/cr.yaml"),
				},
			},
			want: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.ClusterAtPath("clusterregistry/cluster.yaml"),
					k8sobjects.ClusterRoleAtPath("cluster/cr.yaml"),
				},
			},
		},
		{
			// Namespaces and NamespaceSelectors are the only cluster-scoped objects
			// expected under the namespace/ directory, so we want to make sure we
			// don't accidentally assign them a namespace.
			name: "Ignore NamespaceSelector in namespace directory",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader"),
						core.Namespace("foo")),
					k8sobjects.NamespaceSelectorAtPath("namespaces/foo/nss.yaml"),
				},
			},
			want: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Repo(),
					k8sobjects.Namespace("namespaces/foo"),
					k8sobjects.RoleAtPath("namespaces/foo/role.yaml",
						core.Name("reader"),
						core.Namespace("foo")),
					k8sobjects.NamespaceSelectorAtPath("namespaces/foo/nss.yaml"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if errs := ObjectNamespaces(tc.objs); errs != nil {
				t.Errorf("Got ObjectNamespaces() error %v, want nil", errs)
			}
			if diff := cmp.Diff(tc.want, tc.objs, ast.CompareFileObject); diff != "" {
				t.Error(diff)
			}
		})
	}
}
