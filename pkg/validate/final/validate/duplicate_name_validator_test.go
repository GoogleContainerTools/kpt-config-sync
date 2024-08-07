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

	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

func customResource(group, kind, name, namespace string) ast.FileObject {
	gvk := schema.GroupVersionKind{
		Group:   group,
		Version: "v1",
		Kind:    kind,
	}
	return k8sobjects.Unstructured(gvk, core.Name(name), core.Namespace(namespace))
}

func TestDuplicateNames(t *testing.T) {
	testCases := []struct {
		name     string
		objs     []ast.FileObject
		wantErrs status.MultiError
	}{
		{
			name: "Two objects with different names pass",
			objs: []ast.FileObject{
				k8sobjects.Role(core.Name("alice"), core.Namespace("shipping")),
				k8sobjects.Role(core.Name("bob"), core.Namespace("shipping")),
			},
		},
		{
			name: "Two objects with different namespaces pass",
			objs: []ast.FileObject{
				k8sobjects.Role(core.Name("alice"), core.Namespace("shipping")),
				k8sobjects.Role(core.Name("alice"), core.Namespace("production")),
			},
		},
		{
			name: "Two objects with different kinds pass",
			objs: []ast.FileObject{
				k8sobjects.Role(core.Name("alice"), core.Namespace("shipping")),
				k8sobjects.RoleBinding(core.Name("alice"), core.Namespace("shipping")),
			},
		},
		{
			name: "Two objects with different groups pass",
			objs: []ast.FileObject{
				k8sobjects.Role(core.Name("alice"), core.Namespace("shipping")),
				customResource("acme", "Role", "alice", "shipping"),
			},
		},
		{
			name: "Two duplicate namespaced objects fail",
			objs: []ast.FileObject{
				k8sobjects.Role(core.Name("alice"), core.Namespace("shipping")),
				k8sobjects.Role(core.Name("alice"), core.Namespace("shipping")),
			},
			wantErrs: nonhierarchical.NamespaceMetadataNameCollisionError(
				kinds.Role().GroupKind(), "shipping", "alice", k8sobjects.Role()),
		},
		{
			name: "Two duplicate cluster-scoped objects fail",
			objs: []ast.FileObject{
				k8sobjects.ClusterRole(core.Name("alice")),
				k8sobjects.ClusterRole(core.Name("alice")),
			},
			wantErrs: nonhierarchical.ClusterMetadataNameCollisionError(
				kinds.ClusterRole().GroupKind(), "alice", k8sobjects.ClusterRole()),
		},
		{
			name: "Two duplicate namespaces fail",
			objs: []ast.FileObject{
				k8sobjects.Namespace("namespaces/hello"),
				k8sobjects.Namespace("namespaces/hello"),
			},
			wantErrs: nonhierarchical.NamespaceCollisionError(
				"hello", k8sobjects.Namespace("hamespaces/hello")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := DuplicateNames(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got DuplicateNames() error %v, want %v", errs, tc.wantErrs)
			}
		})
	}
}
