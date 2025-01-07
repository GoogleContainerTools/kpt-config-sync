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

package customresources

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func preserveUnknownFields(t *testing.T, preserved bool) core.MetaMutator {
	return func(o client.Object) {
		switch crd := o.(type) {
		case *apiextensionsv1beta1.CustomResourceDefinition:
			crd.Spec.PreserveUnknownFields = &preserved
		case *apiextensionsv1.CustomResourceDefinition:
			crd.Spec.PreserveUnknownFields = preserved
		default:
			t.Fatalf("not a v1beta1.CRD or v1.CRD: %T", o)
		}
	}
}

func TestGetCRDs(t *testing.T) {
	testCases := []struct {
		name string
		objs []ast.FileObject
		want []*apiextensionsv1.CustomResourceDefinition
	}{
		{
			name: "empty is fine",
		},
		{
			name: "ignore non-CRD",
			objs: []ast.FileObject{k8sobjects.Role()},
		},
		{
			name: "one v1beta1 CRD",
			objs: []ast.FileObject{
				k8sobjects.AnvilCRDv1beta1AtPath("crd-anvil.yaml"),
			},
			want: []*apiextensionsv1.CustomResourceDefinition{
				k8sobjects.CRDV1ObjectForGVK(
					kinds.Anvil(), apiextensionsv1.NamespaceScoped),
			},
		},
		{
			name: "one v1 CRD",
			objs: []ast.FileObject{
				k8sobjects.AnvilCRDv1AtPath("crd-anvil.yaml"),
			},
			want: []*apiextensionsv1.CustomResourceDefinition{
				// The default if unspecified is true/true for served/storage.
				k8sobjects.CRDV1ObjectForGVK(
					kinds.Anvil(), apiextensionsv1.NamespaceScoped,
					preserveUnknownFields(t, false)),
			},
		},
		{
			name: "both CRD versions",
			objs: []ast.FileObject{
				k8sobjects.FileObject(
					k8sobjects.CRDV1beta1UnstructuredForGVK(
						kinds.Role(), apiextensionsv1.NamespaceScoped),
					"crd-role.yaml"),
				k8sobjects.FileObject(
					k8sobjects.CRDV1UnstructuredForGVK(
						kinds.ClusterRole(), apiextensionsv1.ClusterScoped),
					"crd-clusterrole.yaml"),
			},
			want: []*apiextensionsv1.CustomResourceDefinition{
				// GetCRDs sorts by object name.
				k8sobjects.CRDV1ObjectForGVK(
					kinds.ClusterRole(), apiextensionsv1.ClusterScoped,
					preserveUnknownFields(t, false)),
				// When converting from v1 to v1beta1, spec.version is converted
				// to spec.versions, with served & storage set to true.
				k8sobjects.CRDV1ObjectForGVK(
					kinds.Role(), apiextensionsv1.NamespaceScoped),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := GetCRDs(tc.objs, core.Scheme)

			if err != nil {
				t.Fatal(err)
			}

			opts := []cmp.Option{
				cmpopts.EquateEmpty(),
				cmpopts.IgnoreFields(apiextensionsv1.CustomResourceDefinition{}, "TypeMeta"),
			}
			if diff := cmp.Diff(tc.want, actual, opts...); diff != "" {
				t.Error(diff)
			}
		})
	}
}
