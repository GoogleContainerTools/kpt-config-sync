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

package decode

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/davecgh/go-spew/spew"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
)

func TestDecodeResources(t *testing.T) {
	testCases := []struct {
		name            string
		genericResource []v1.GenericResources
		want            map[schema.GroupVersionKind][]*unstructured.Unstructured
	}{
		{
			name: "no resource",
			genericResource: []v1.GenericResources{
				{
					Kind:     "ObjectTest",
					Versions: []v1.GenericVersionResources{},
				},
			},
			want: map[schema.GroupVersionKind][]*unstructured.Unstructured{},
		},
		{
			name: "one resource",
			genericResource: []v1.GenericResources{
				{
					Kind: "ObjectTest",
					Versions: []v1.GenericVersionResources{{
						Version: "v1test",
						Objects: []runtime.RawExtension{{
							Raw: []byte(`{"kind":"ObjectTest","apiVersion":"v1test"}`),
						}},
					}},
				},
			},
			want: map[schema.GroupVersionKind][]*unstructured.Unstructured{
				{Group: "", Version: "v1test", Kind: "ObjectTest"}: {
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind":       "ObjectTest",
							"apiVersion": "v1test",
						},
					},
				},
			},
		},
		{
			name: "one resource that is already an Object",
			genericResource: []v1.GenericResources{
				{
					Kind: "ObjectTest",
					Versions: []v1.GenericVersionResources{{
						Version: "v1test",
						Objects: []runtime.RawExtension{{
							Object: &unstructured.Unstructured{
								Object: map[string]interface{}{
									"kind":       "ObjectTest",
									"apiVersion": "v1test",
								},
							},
						}},
					}},
				},
			},
			want: map[schema.GroupVersionKind][]*unstructured.Unstructured{
				{Group: "", Version: "v1test", Kind: "ObjectTest"}: {
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind":       "ObjectTest",
							"apiVersion": "v1test",
						},
					},
				},
			},
		},
		{
			name: "multiple resources",
			genericResource: []v1.GenericResources{
				{
					Kind: "ObjectTest",
					Versions: []v1.GenericVersionResources{{
						Version: "v1test",
						Objects: []runtime.RawExtension{{
							Raw: []byte(`{"kind":"ObjectTest","apiVersion":"v1test"}`),
						}},
					}},
				},
				{
					Group: "foo.com",
					Kind:  "Bar",
					Versions: []v1.GenericVersionResources{{
						Version: "v1",
						Objects: []runtime.RawExtension{
							{
								Raw: []byte(`{"kind":"Bar","apiVersion":"foo.com/v1","metadata":{"name": "my-bar"},"spec":{"foo":"baz"}}`),
							},
							{
								Raw: []byte(`{"kind":"Bar","apiVersion":"foo.com/v1","metadata":{"name": "my-other-bar"},
"spec":{"foo":"baz"}}`),
							},
						},
					}},
				},
			},
			want: map[schema.GroupVersionKind][]*unstructured.Unstructured{
				{Group: "", Version: "v1test", Kind: "ObjectTest"}: {
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind":       "ObjectTest",
							"apiVersion": "v1test",
						},
					},
				},
				{Group: "foo.com", Version: "v1", Kind: "Bar"}: {
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind":       "Bar",
							"apiVersion": "foo.com/v1",
							"metadata": map[string]interface{}{
								"name": "my-bar",
							},
							"spec": map[string]interface{}{
								"foo": "baz",
							},
						},
					},
					&unstructured.Unstructured{
						Object: map[string]interface{}{
							"kind":       "Bar",
							"apiVersion": "foo.com/v1",
							"metadata": map[string]interface{}{
								"name": "my-other-bar",
							},
							"spec": map[string]interface{}{
								"foo": "baz",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			for w := range tc.want {
				scheme.AddKnownTypeWithName(w, &unstructured.Unstructured{})
			}
			d := NewGenericResourceDecoder(scheme)
			got, err := d.DecodeResources(tc.genericResource)
			if err != nil {
				t.Errorf("Could not decode: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got: %v\nwant: %v", spew.Sdump(got), spew.Sdump(tc.want))
			}
		})
	}
}
