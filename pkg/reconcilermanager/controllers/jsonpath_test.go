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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/jsonpath"
	"sigs.k8s.io/yaml"
)

var o1y = `
apiVersion: v1
kind: Pod
metadata:
  name: pod-name
  namespace: pod-namespace
map:
  a:
  - "1"
  - "2"
  - "3"
  b: null
  c:
  - x
  - y
  - z
  e: 1
  f: false
entries:
- name: a
  value: x
- name: b
  value: "y"
- name: c
  value: z
`

func TestDeleteFields(t *testing.T) {
	o1 := yamlToUnstructured(t, o1y)

	testCases := map[string]struct {
		obj    *unstructured.Unstructured
		path   string
		values []interface{}
		errMsg string
	}{
		"string in map": {
			obj:    o1,
			path:   "$.metadata.name",
			values: []interface{}{"pod-name"},
		},
		"bool in map": {
			obj:    o1,
			path:   "$.map.f",
			values: []interface{}{false},
		},
		"int in map": {
			obj:    o1,
			path:   "$.map.e",
			values: []interface{}{1},
		},
		"string in array in map": {
			obj:    o1,
			path:   "$.map.c[2]",
			values: []interface{}{"z"},
		},
		"nil in map": {
			obj:    o1,
			path:   "$.map.b",
			values: []interface{}{nil},
		},
		"array in map": {
			obj:    o1,
			path:   "$.map.a",
			values: []interface{}{[]interface{}{"1", "2", "3"}},
		},
		"field selector": {
			obj:    o1,
			path:   `$.entries[?(@.name=="b")].value`,
			values: []interface{}{"y"},
		},
		"multi-field selector": {
			obj:    o1,
			path:   `$.entries[?(@.name=="a" || @.name=="c")].value`,
			values: []interface{}{"x", "z"},
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			testCtx := []interface{}{"path: %s\nobject:\n%s", tc.path, toYaml(t, tc.obj.Object)}
			values, err := jsonpath.Get(tc.obj.Object, tc.path)
			require.NoError(t, err, testCtx...)
			require.Equal(t, tc.values, values, testCtx...)
			err = deleteFields(tc.obj.Object, tc.path)
			require.NoError(t, err, testCtx...)
			values, err = jsonpath.Get(tc.obj.Object, tc.path)
			require.NoError(t, err, testCtx...)
			require.Equal(t, []interface{}{}, values, testCtx...)
		})
	}
}

func toYaml(t *testing.T, in interface{}) string {
	yamlBytes, err := yaml.Marshal(in)
	require.NoError(t, err)
	return string(yamlBytes)
}

func yamlToUnstructured(t *testing.T, yml string) *unstructured.Unstructured {
	m := make(map[string]interface{})
	err := yaml.Unmarshal([]byte(yml), &m)
	if err != nil {
		t.Fatalf("error parsing yaml: %v", err)
		return nil
	}
	return &unstructured.Unstructured{Object: m}
}

func yamlToDeployment(t *testing.T, yml string) *appsv1.Deployment {
	d := &appsv1.Deployment{}
	err := yaml.Unmarshal([]byte(yml), d)
	if err != nil {
		t.Fatalf("error parsing yaml: %v", err)
		return nil
	}
	return d
}
