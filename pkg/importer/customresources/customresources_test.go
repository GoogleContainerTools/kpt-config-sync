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
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func servedStorage(t *testing.T, served, storage bool) core.MetaMutator {
	return func(o client.Object) {
		switch crd := o.(type) {
		case *apiextensionsv1.CustomResourceDefinition:
			crd.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{
				{
					Served:  served,
					Storage: storage,
				},
			}
		default:
			t.Fatalf("not a v1.CRD: %T", o)
		}
	}
}

func preserveUnknownFields(t *testing.T, preserved bool) core.MetaMutator {
	return func(o client.Object) {
		switch crd := o.(type) {
		case *apiextensionsv1.CustomResourceDefinition:
			crd.Spec.PreserveUnknownFields = preserved
		default:
			t.Fatalf("not a v1.CRD: %T", o)
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
			objs: []ast.FileObject{fake.Role()},
		},
		{
			name: "v1 CRD",
			objs: []ast.FileObject{
				fake.CustomResourceDefinition(servedStorage(t, true, true)),
			},
			want: []*apiextensionsv1.CustomResourceDefinition{
				// The default if unspecified is true/true for served/storage.
				fake.CustomResourceDefinitionObject(servedStorage(t, true, true), preserveUnknownFields(t, false)),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := GetCRDs(tc.objs)

			if err != nil {
				t.Fatal(err)
			}

			if diff := cmp.Diff(tc.want, actual, cmpopts.EquateEmpty(), cmpopts.IgnoreFields(apiextensionsv1.CustomResourceDefinition{}, "TypeMeta")); diff != "" {
				t.Error(diff)
			}
		})
	}
}
