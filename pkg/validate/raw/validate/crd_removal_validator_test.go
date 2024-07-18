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

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

func crd(gvk schema.GroupVersionKind) *v1beta1.CustomResourceDefinition {
	return &v1beta1.CustomResourceDefinition{
		Spec: v1beta1.CustomResourceDefinitionSpec{
			Group: gvk.Group,
			Names: v1beta1.CustomResourceDefinitionNames{
				Kind: gvk.Kind,
			},
		},
	}
}

func crdFileObject(t *testing.T, gvk schema.GroupVersionKind) ast.FileObject {
	t.Helper()
	u := k8sobjects.CustomResourceDefinitionV1Beta1Unstructured()
	if err := unstructured.SetNestedField(u.Object, gvk.Group, "spec", "group"); err != nil {
		t.Fatal(err)
	}
	if err := unstructured.SetNestedField(u.Object, gvk.Kind, "spec", "names", "kind"); err != nil {
		t.Fatal(err)
	}
	return k8sobjects.FileObject(u, "crd.yaml")
}

func TestRemovedCRDs(t *testing.T) {
	testCases := []struct {
		name    string
		objs    *fileobjects.Raw
		wantErr status.MultiError
	}{
		{
			name: "no previous or current CRDs",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.AnvilAtPath("anvil1.yaml"),
				},
			},
		},
		{
			name: "add a CRD",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					crdFileObject(t, kinds.Anvil()),
					k8sobjects.AnvilAtPath("anvil1.yaml"),
				},
			},
		},
		{
			name: "keep a CRD",
			objs: &fileobjects.Raw{
				PreviousCRDs: []*v1beta1.CustomResourceDefinition{
					crd(kinds.Anvil()),
				},
				Objects: []ast.FileObject{
					crdFileObject(t, kinds.Anvil()),
					k8sobjects.AnvilAtPath("anvil1.yaml"),
				},
			},
		},
		{
			name: "remove an unused CRD",
			objs: &fileobjects.Raw{
				PreviousCRDs: []*v1beta1.CustomResourceDefinition{
					crd(kinds.Anvil()),
				},
				Objects: []ast.FileObject{
					k8sobjects.Role(),
				},
			},
		},
		{
			name: "remove an in-use CRD",
			objs: &fileobjects.Raw{
				PreviousCRDs: []*v1beta1.CustomResourceDefinition{
					crd(kinds.Anvil()),
				},
				Objects: []ast.FileObject{
					k8sobjects.AnvilAtPath("anvil1.yaml"),
				},
			},
			wantErr: nonhierarchical.UnsupportedCRDRemovalError(k8sobjects.AnvilAtPath("anvil1.yaml")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := RemovedCRDs(tc.objs)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got RemovedCRDs() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
