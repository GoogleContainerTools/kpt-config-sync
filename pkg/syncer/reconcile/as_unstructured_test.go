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

package reconcile

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAsUnstructured_AddsStatus(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
	}{
		{
			name: "Namespace",
			obj:  &corev1.Namespace{},
		},
		{
			name: "Service",
			obj:  &corev1.Service{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := AsUnstructured(tc.obj)
			if err != nil {
				t.Fatalf("unable to convert %T to Unstructured: %v", tc.obj, err)
			}

			// Yes, we don't like this behavior.
			// These checks validate the bug.
			if _, hasStatus := u.Object["status"]; !hasStatus {
				t.Errorf("got .status undefined, want defined: %s", log.AsJSON(u))
			}

			metadata := u.Object["metadata"].(map[string]interface{})
			if _, hasCreationTimestamp := metadata["creationTimestamp"]; !hasCreationTimestamp {
				t.Errorf("got .metadata.creationTimestamp undefined, want defined: %s", log.AsJSON(u))
			}
		})
	}
}

func TestAsUnstructured_AddsGVK(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
	}{
		{
			name: "Namespace",
			obj:  &corev1.Namespace{},
		},
		{
			name: "Service",
			obj:  &corev1.Service{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := AsUnstructured(tc.obj)
			if err != nil {
				t.Fatalf("unable to convert %T to Unstructured: %v", tc.obj, err)
			}

			if u.GetObjectKind().GroupVersionKind().Empty() {
				t.Errorf("got .kind & .apiVersion undefined, want defined: %s", log.AsJSON(u))
			}

			// Yes, we don't like this behavior.
			// These checks validate the bug.
			if _, hasStatus := u.Object["status"]; !hasStatus {
				t.Errorf("got .status undefined, want defined: %s", log.AsJSON(u))
			}

			metadata := u.Object["metadata"].(map[string]interface{})
			if _, hasCreationTimestamp := metadata["creationTimestamp"]; !hasCreationTimestamp {
				t.Errorf("got .metadata.creationTimestamp undefined, want defined: %s", log.AsJSON(u))
			}
		})
	}
}

func TestAsUnstructuredSanitized_DoesNotAddStatus(t *testing.T) {
	testCases := []struct {
		name string
		obj  client.Object
	}{
		{
			name: "Namespace",
			obj:  &corev1.Namespace{},
		},
		{
			name: "Service",
			obj:  &corev1.Service{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := AsUnstructuredSanitized(tc.obj)
			if err != nil {
				t.Fatalf("unable to convert %T to Unstructured: %v", tc.obj, err)
			}

			if _, hasStatus := u.Object["status"]; hasStatus {
				t.Errorf("got .status defined, want undefined: %s", log.AsJSON(u))
			}

			metadata := u.Object["metadata"].(map[string]interface{})
			if _, hasCreationTimestamp := metadata["creationTimestamp"]; hasCreationTimestamp {
				t.Errorf("got .metadata.creationTimestamp defined, want undefined: %s", log.AsJSON(u))
			}
		})
	}
}

func TestAsUnstructuredSanitized_DeepCopy(t *testing.T) {
	wantName := "foo"

	testCases := []struct {
		name string
		obj  client.Object
	}{
		{
			name: "Namespace as object",
			obj:  &corev1.Namespace{},
		},
		{
			name: "Namespace as unstructured",
			obj:  newUnstructured(kinds.Namespace()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tc.obj.SetName(wantName)

			u, err := AsUnstructuredSanitized(tc.obj)
			if err != nil {
				t.Fatalf("unable to convert %T to Unstructured: %v", tc.obj, err)
			}
			// Verify the name was unchanged by conversion to unstructured.
			if u.GetName() != wantName {
				t.Errorf("got name %q, want name %q", u.GetName(), wantName)
			}

			// Modify the original name and verify the unstructured name is still unchanged.
			tc.obj.SetName("bar")
			if u.GetName() != wantName {
				t.Errorf("got name %q, want name %q", u.GetName(), wantName)
			}
		})
	}
}

func newUnstructured(gvk schema.GroupVersionKind) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)
	return u
}
