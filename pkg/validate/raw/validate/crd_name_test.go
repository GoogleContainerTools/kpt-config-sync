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
	"strings"
	"testing"

	"github.com/pkg/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func crdv1beta1(name string, gvk schema.GroupVersionKind) ast.FileObject {
	result := fake.CustomResourceDefinitionV1Beta1Object()
	result.Name = name
	result.Spec.Group = gvk.Group
	result.Spec.Names = apiextensionsv1beta1.CustomResourceDefinitionNames{
		Plural: strings.ToLower(gvk.Kind) + "s",
		Kind:   gvk.Kind,
	}
	return fake.FileObject(result, "crd.yaml")
}

func crdv1(name string, gvk schema.GroupVersionKind) ast.FileObject {
	result := fake.CustomResourceDefinitionV1Object()
	result.Name = name
	result.Spec.Group = gvk.Group
	result.Spec.Names = apiextensionsv1.CustomResourceDefinitionNames{
		Plural: strings.ToLower(gvk.Kind) + "s",
		Kind:   gvk.Kind,
	}
	return fake.FileObject(result, "crd.yaml")
}

func TestValidCRDName(t *testing.T) {
	testCases := []struct {
		name string
		obj  ast.FileObject
		want status.Error
	}{
		// v1beta1 CRDs
		{
			name: "v1beta1 valid name",
			obj:  crdv1beta1("anvils.acme.com", kinds.Anvil()),
		},
		{
			name: "v1beta1 non plural",
			obj:  crdv1beta1("anvil.acme.com", kinds.Anvil()),
			want: fake.Error(nonhierarchical.InvalidCRDNameErrorCode),
		},
		{
			name: "v1beta1 missing group",
			obj:  crdv1beta1("anvils", kinds.Anvil()),
			want: fake.Error(nonhierarchical.InvalidCRDNameErrorCode),
		},
		// v1 CRDs
		{
			name: "v1 valid name",
			obj:  crdv1("anvils.acme.com", kinds.Anvil()),
		},
		{
			name: "v1 non plural",
			obj:  crdv1("anvil.acme.com", kinds.Anvil()),
			want: fake.Error(nonhierarchical.InvalidCRDNameErrorCode),
		},
		{
			name: "v1 missing group",
			obj:  crdv1("anvils", kinds.Anvil()),
			want: fake.Error(nonhierarchical.InvalidCRDNameErrorCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := nonhierarchical.ValidateCRDName(tc.obj)
			if !errors.Is(err, tc.want) {
				t.Errorf("got ValidateCRDName() error %v, want %v", err, tc.want)
			}
		})
	}
}
