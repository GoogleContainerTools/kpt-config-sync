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

package clusterconfig

import (
	"fmt"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/testerrors"
)

func generateMalformedCRD(t *testing.T) *unstructured.Unstructured {
	u := k8sobjects.CRDV1beta1UnstructuredForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped)

	// the `spec.group` field should be a string
	// set it to a bool to construct a malformed CRD
	if err := unstructured.SetNestedField(u.Object, false, "spec", "group"); err != nil {
		t.Fatalf("failed to set the generation field: %T", u)
	}
	return u
}

func TestToCRD(t *testing.T) {
	testCases := []struct {
		name    string
		obj     *unstructured.Unstructured
		wantErr status.Error
	}{
		{
			name:    "well-formed v1beta1 CRD",
			obj:     k8sobjects.CRDV1beta1UnstructuredForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped),
			wantErr: nil,
		},
		{
			name:    "well-formed v1 CRD",
			obj:     k8sobjects.CRDV1UnstructuredForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped),
			wantErr: nil,
		},
		{
			name: "mal-formed CRD",
			obj:  generateMalformedCRD(t),
			wantErr: MalformedCRDError(
				fmt.Errorf("unable to convert unstructured object to %v: %v",
					kinds.CustomResourceDefinition().WithVersion("v1beta1"),
					fmt.Errorf("unrecognized type: string")),
				generateMalformedCRD(t)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ToCRD(tc.obj, core.Scheme)
			testerrors.AssertEqual(t, tc.wantErr, err)
		})
	}
}
