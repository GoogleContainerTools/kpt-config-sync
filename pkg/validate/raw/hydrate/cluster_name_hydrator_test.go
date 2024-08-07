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
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

func TestClusterName(t *testing.T) {
	testCases := []struct {
		name string
		objs *fileobjects.Raw
		want *fileobjects.Raw
	}{
		{
			name: "Hydrate with cluster name",
			objs: &fileobjects.Raw{
				ClusterName: "hello-world",
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml"),
					k8sobjects.RoleBindingAtPath("namespaces/foo/rolebinding.yaml"),
				},
			},
			want: &fileobjects.Raw{
				ClusterName: "hello-world",
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml",
						core.Annotation(metadata.ClusterNameAnnotationKey, "hello-world")),
					k8sobjects.RoleBindingAtPath("namespaces/foo/rolebinding.yaml",
						core.Annotation(metadata.ClusterNameAnnotationKey, "hello-world")),
				},
			},
		},
		{
			name: "Hydrate with empty cluster name",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml"),
					k8sobjects.RoleBindingAtPath("namespaces/foo/rolebinding.yaml"),
				},
			},
			want: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml"),
					k8sobjects.RoleBindingAtPath("namespaces/foo/rolebinding.yaml"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ClusterName(tc.objs)
			if errs != nil {
				t.Errorf("Got ClusterName() error %v, want nil", errs)
			}
			if diff := cmp.Diff(tc.want, tc.objs, ast.CompareFileObject); diff != "" {
				t.Error(diff)
			}
		})
	}
}
