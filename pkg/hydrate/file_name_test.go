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
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
)

func cluster(name string) core.MetaMutator {
	return core.Annotation(metadata.ClusterNameAnnotationKey, name)
}

func TestToFileObjects(t *testing.T) {
	testCases := []struct {
		name     string
		objects  []ast.FileObject
		expected []ast.FileObject
	}{
		{
			name: "nil returns empty",
		},
		{
			name: "namespaced role works",
			objects: []ast.FileObject{
				fake.Role(core.Name("alice"), core.Namespace("prod"), cluster("na-1")),
			},
			expected: []ast.FileObject{
				fake.FileObject(fake.RoleObject(core.Name("alice"), core.Namespace("prod"), cluster("na-1")), "na-1/prod/role_alice.yaml"),
			},
		},
		{
			name: "non-namespaced clusterrolebinding works",
			objects: []ast.FileObject{
				fake.ClusterRoleBinding(core.Name("alice"), cluster("eu-2")),
			},
			expected: []ast.FileObject{
				fake.FileObject(fake.ClusterRoleBindingObject(core.Name("alice"), cluster("eu-2")), "eu-2/clusterrolebinding_alice.yaml"),
			},
		},
		{
			name: "conflict resolved",
			objects: []ast.FileObject{
				fake.Unstructured(schema.GroupVersionKind{
					Group: "rbac",
					Kind:  "ClusterRole",
				}, core.Name("alice")),
				fake.Unstructured(schema.GroupVersionKind{
					Group: "oauth",
					Kind:  "ClusterRole",
				}, core.Name("alice")),
			},
			expected: []ast.FileObject{
				fake.UnstructuredAtPath(schema.GroupVersionKind{
					Group: "rbac",
					Kind:  "ClusterRole",
				}, "defaultcluster/clusterrole.rbac_alice.yaml", core.Name("alice")),
				fake.UnstructuredAtPath(schema.GroupVersionKind{
					Group: "oauth",
					Kind:  "ClusterRole",
				}, "defaultcluster/clusterrole.oauth_alice.yaml", core.Name("alice")),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := generateUniqueFileNames("yaml", true, tc.objects...)
			if diff := cmp.Diff(tc.expected, actual, cmpopts.EquateEmpty(), ast.CompareFileObject, cmpopts.SortSlices(func(x, y ast.FileObject) bool {
				return x.SlashPath() < y.SlashPath()
			})); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
