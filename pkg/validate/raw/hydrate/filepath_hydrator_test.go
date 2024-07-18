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
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

const dir = "acme/"

func TestFilepath(t *testing.T) {
	testCases := []struct {
		name string
		objs *fileobjects.Raw
		want *fileobjects.Raw
	}{
		{
			name: "Hydrate with filepaths",
			objs: &fileobjects.Raw{
				PolicyDir: cmpath.RelativeSlash(dir),
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml", core.Name("reader")),
					k8sobjects.RoleAtPath("namespaces/role.yaml", core.Name("writer")),
					k8sobjects.Namespace("namespaces/hello"),
					k8sobjects.RoleBindingAtPath("namespaces/hello/binding.yaml", core.Name("bind-writer")),
				},
			},
			want: &fileobjects.Raw{
				PolicyDir: cmpath.RelativeSlash(dir),
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml",
						core.Name("reader"),
						core.Annotation(metadata.SourcePathAnnotationKey, dir+"cluster/clusterrole.yaml")),
					k8sobjects.RoleAtPath("namespaces/role.yaml",
						core.Name("writer"),
						core.Annotation(metadata.SourcePathAnnotationKey, dir+"namespaces/role.yaml")),
					k8sobjects.Namespace("namespaces/hello",
						core.Annotation(metadata.SourcePathAnnotationKey, dir+"namespaces/hello/namespace.yaml")),
					k8sobjects.RoleBindingAtPath("namespaces/hello/binding.yaml",
						core.Name("bind-writer"),
						core.Annotation(metadata.SourcePathAnnotationKey, dir+"namespaces/hello/binding.yaml")),
				},
			},
		},
		{
			name: "Preserve existing annotations",
			objs: &fileobjects.Raw{
				PolicyDir: cmpath.RelativeSlash(dir),
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml",
						core.Name("reader"),
						core.Annotation("color", "blue")),
				},
			},
			want: &fileobjects.Raw{
				PolicyDir: cmpath.RelativeSlash(dir),
				Objects: []ast.FileObject{
					k8sobjects.ClusterRoleAtPath("cluster/clusterrole.yaml",
						core.Name("reader"),
						core.Annotation("color", "blue"),
						core.Annotation(metadata.SourcePathAnnotationKey, dir+"cluster/clusterrole.yaml")),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Filepath(tc.objs); err != nil {
				t.Errorf("Got Filepath() error %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, tc.objs, ast.CompareFileObject); diff != "" {
				t.Error(diff)
			}
		})
	}
}
