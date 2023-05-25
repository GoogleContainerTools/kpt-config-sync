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

package fake

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

func TestMinimizeUnstructured(t *testing.T) {
	testcases := []struct {
		name          string
		input, output *unstructured.Unstructured
	}{
		{
			name: "complex",
			input: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "RepoSync",
					"apiVersion": "configsync.gke.io/v1beta1",
					"metadata": map[string]interface{}{
						"name":              "rs",
						"uid":               "1",
						"resourceVersion":   "1",
						"generation":        int64(1),
						"namespace":         "test-namespace",
						"creationTimestamp": nil,
					},
					"spec": map[string]interface{}(nil),
					"status": map[string]interface{}{
						"source":    map[string]interface{}(nil),
						"rendering": map[string]interface{}(nil),
						"sync":      map[string]interface{}(nil),
						"conditions": []interface{}{
							map[string]interface{}{
								"errors": []interface{}{},
							},
						},
					},
				},
			},
			output: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"kind":       "RepoSync",
					"apiVersion": "configsync.gke.io/v1beta1",
					"metadata": map[string]interface{}{
						"name":            "rs",
						"uid":             "1",
						"resourceVersion": "1",
						"generation":      int64(1),
						"namespace":       "test-namespace",
					},
				},
			},
		},
		{
			name: "simple: nil map",
			input: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"spec": map[string]interface{}(nil),
				},
			},
			output: &unstructured.Unstructured{
				Object: nil,
			},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			out := tc.input.DeepCopy()
			MinimizeUnstructured(out)
			testutil.AssertEqual(t, tc.output, out)
		})
	}
}
