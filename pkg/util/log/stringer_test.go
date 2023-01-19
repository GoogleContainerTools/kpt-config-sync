// Copyright 2023 Google LLC
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

package log

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAsYAML(t *testing.T) {
	testCases := []struct {
		name           string
		input          interface{}
		expectedOutput string
	}{
		{
			name: "typed Namespace",
			input: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
				},
			},
			expectedOutput: `apiVersion: v1
kind: Namespace
metadata:
  creationTimestamp: null
  name: example
spec: {}
status: {}
`,
		},
		{
			name: "unstructured namespace",
			input: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			expectedOutput: `apiVersion: v1
kind: Namespace
metadata:
  name: example
`,
		},
		{
			name: "unstructured object missing kind",
			input: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			expectedOutput: `apiVersion: v1
metadata:
  name: example
`,
		},
		{
			name: "core.ID (non-object compound struct)",
			input: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			expectedOutput: `Group: ""
Kind: Namespace
Name: example
Namespace: ""
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out := AsYAML(tc.input).String()
			testutil.AssertEqual(t, tc.expectedOutput, out)
		})
	}
}

func TestAsYAMLDiff(t *testing.T) {
	testCases := []struct {
		name           string
		old, new       interface{}
		expectedOutput string
	}{
		{
			name: "typed Namespace no diff",
			old: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
				},
			},
			new: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
				},
			},
			expectedOutput: ``, // empty-string means no diff
		},
		{
			name: "typed Namespace with diff",
			old: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Labels: map[string]string{
						"key1": "value1",
					},
				},
			},
			new: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "example",
					Labels: map[string]string{
						"key2": "value2",
					},
				},
			},
			expectedOutput: ` apiVersion: v1
 kind: Namespace
 metadata:
   creationTimestamp: null
   labels:
-    key1: value1
+    key2: value2
   name: example
 spec: {}
 status: {}
 `,
		},
		{
			name: "unstructured namespace with no diff",
			old: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			new: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
					},
				},
			},
			expectedOutput: ``, // empty-string means no diff
		},
		{
			name: "unstructured namespace with diff",
			old: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
						"labels": map[string]string{
							"key1": "value1",
						},
					},
				},
			},
			new: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "Namespace",
					"metadata": map[string]interface{}{
						"name": "example",
						"labels": map[string]string{
							"key2": "value2",
						},
					},
				},
			},
			expectedOutput: ` apiVersion: v1
 kind: Namespace
 metadata:
   labels:
-    key1: value1
+    key2: value2
   name: example
 `,
		},
		{
			name: "core.ID (non-object compound struct) with no diff",
			old: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			new: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			expectedOutput: ``,
		},
		{
			name: "core.ID (non-object compound struct) with diff",
			old: core.ID{
				GroupKind: schema.GroupKind{
					Group: "",
					Kind:  "Namespace",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			new: core.ID{
				GroupKind: schema.GroupKind{
					Group: "apps",
					Kind:  "Deployment",
				},
				ObjectKey: client.ObjectKey{
					Name:      "example",
					Namespace: "",
				},
			},
			expectedOutput: `-Group: ""
-Kind: Namespace
+Group: apps
+Kind: Deployment
 Name: example
 Namespace: ""
 `,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			out := AsYAMLDiff(tc.old, tc.new).String()
			testutil.AssertEqual(t, tc.expectedOutput, out)
		})
	}
}
