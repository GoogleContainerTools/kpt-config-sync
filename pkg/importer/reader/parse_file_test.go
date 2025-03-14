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

package reader

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
)

func TestParseYAMLFile(t *testing.T) {
	testCases := []struct {
		name      string
		contents  string
		expected  []*unstructured.Unstructured
		expectErr bool
	}{
		{
			name: "empty file",
		},
		{
			name: "empty document with leading separator",
			contents: `---
`,
		},
		{
			name: "multiple empty documents",
			contents: `---
---
`,
		},
		{
			name: "empty document with no separator",
			contents: `
`,
		},
		{
			name:     "empty document with leading comment",
			contents: `# comment`,
		},
		{
			name: "empty document with comment",
			contents: `
# comment
`,
		},
		{
			name: "empty document with comment and separator",
			contents: `

---
# comment
`,
		},
		{
			name: "one document",
			contents: `apiVersion: v1
kind: Namespace
metadata:
  name: shipping
`,
			expected: []*unstructured.Unstructured{
				k8sobjects.UnstructuredObject(kinds.Namespace(), core.Name("shipping")),
			},
		}, {
			name: "one document with triple-dash in a string",
			contents: `apiVersion: v1
kind: Namespace
metadata:
  name: shipping
  labels:
    "a": "---"
`,
			expected: []*unstructured.Unstructured{
				k8sobjects.UnstructuredObject(kinds.Namespace(), core.Name("shipping"), core.Label("a", "---")),
			},
		},
		{
			name: "two documents",
			contents: `apiVersion: v1
kind: Namespace
metadata:
  name: shipping
---
apiVersion: rbac/v1
kind: Role
metadata:
  name: admin
  namespace: shipping
rules:
- apiGroups: [rbac]
  verbs: [all]
`,
			expected: []*unstructured.Unstructured{
				k8sobjects.UnstructuredObject(kinds.Namespace(), core.Name("shipping")),
				{
					Object: map[string]interface{}{
						"apiVersion": "rbac/v1",
						"kind":       "Role",
						"metadata": map[string]interface{}{
							"name":        "admin",
							"namespace":   "shipping",
							"labels":      make(map[string]interface{}),
							"annotations": make(map[string]interface{}),
						},
						"rules": []interface{}{
							map[string]interface{}{
								"apiGroups": []interface{}{"rbac"},
								"verbs":     []interface{}{"all"},
							},
						},
					},
				},
			},
		},
		{
			name: "begin with document separator",
			contents: `---
apiVersion: v1
kind: Namespace
metadata:
  name: foo`,
			expected: []*unstructured.Unstructured{
				k8sobjects.UnstructuredObject(kinds.Namespace(), core.Name("foo")),
			},
		},
		{
			name: "indented document separator",
			contents: `---
apiVersion: v1
kind: Namespace
metadata:
  name: foo
  labels:
    a: >
      this
      ---
      is not a separator
`,
			expected: []*unstructured.Unstructured{
				k8sobjects.UnstructuredObject(kinds.Namespace(), core.Name("foo"),
					core.Label("a", "this --- is not a separator\n")),
			},
		},
		{
			name: "ignore after document end indicator",
			contents: `---
apiVersion: v1
kind: Namespace
metadata:
  name: foo
...
This is a comment and doesn't need to be valid YAML.
---
apiVersion: v1
kind: Namespace
metadata:
  name: bar
`,
			expected: []*unstructured.Unstructured{
				k8sobjects.UnstructuredObject(kinds.Namespace(), core.Name("foo")),
				k8sobjects.UnstructuredObject(kinds.Namespace(), core.Name("bar")),
			},
		},
		{
			name: "ignore local configuration",
			contents: `---
apiVersion: v1
kind: Namespace
metadata:
  name: foo
  annotations:
    config.kubernetes.io/local-config: "true"
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := parseYAMLFile([]byte(tc.contents))
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			} else if err != nil {
				t.Fatal(fmt.Errorf("unexpected error: %w", err))
			}

			for _, a := range actual {
				if a.GetLabels() == nil {
					a.SetLabels(make(map[string]string))
				}
				if a.GetAnnotations() == nil {
					a.SetAnnotations(make(map[string]string))
				}
			}
			if diff := cmp.Diff(tc.expected, actual, cmpopts.EquateEmpty()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestParseJsonFile(t *testing.T) {
	testCases := []struct {
		name      string
		contents  string
		expected  []*unstructured.Unstructured
		expectErr bool
	}{
		{
			name: "empty file",
		},
		{
			name: "one object",
			contents: `{
  "apiVersion": "rbac/v1",
  "kind": "Role",
  "metadata": {
    "name": "admin",
    "namespace": "shipping"
  },
  "rules": [
    {
      "apiGroups": ["rbac"],
      "verbs": ["all"]
    }
  ]
}
`,
			expected: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "rbac/v1",
						"kind":       "Role",
						"metadata": map[string]interface{}{
							"name":      "admin",
							"namespace": "shipping",
						},
						"rules": []interface{}{
							map[string]interface{}{
								"apiGroups": []interface{}{"rbac"},
								"verbs":     []interface{}{"all"},
							},
						},
					},
				},
			},
		},
		{
			name: "one object (with local-config: false)",
			contents: `{
  "apiVersion": "rbac/v1",
  "kind": "Role",
  "metadata": {
    "name": "admin",
    "namespace": "shipping",
    "annotations": {
      "config.kubernetes.io/local-config": "false"
    }
  },
  "rules": [
    {
      "apiGroups": ["rbac"],
      "verbs": ["all"]
    }
  ]
}
`,
			expected: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"apiVersion": "rbac/v1",
						"kind":       "Role",
						"metadata": map[string]interface{}{
							"name":      "admin",
							"namespace": "shipping",
							"annotations": map[string]interface{}{
								"config.kubernetes.io/local-config": "false",
							},
						},
						"rules": []interface{}{
							map[string]interface{}{
								"apiGroups": []interface{}{"rbac"},
								"verbs":     []interface{}{"all"},
							},
						},
					},
				},
			},
		},
		{
			name: "ignore local configuration",
			contents: `{
  "apiVersion": "rbac/v1",
  "kind": "Role",
  "metadata": {
    "name": "admin",
    "namespace": "shipping",
    "annotations": {
      "config.kubernetes.io/local-config": "true"
    }
  },
  "rules": [
    {
      "apiGroups": ["rbac"],
      "verbs": ["all"]
    }
  ]
}
`,
		},
		{
			name: "ignore local configuration (with local-config: True)",
			contents: `{
  "apiVersion": "rbac/v1",
  "kind": "Role",
  "metadata": {
    "name": "admin",
    "namespace": "shipping",
    "annotations": {
      "config.kubernetes.io/local-config": "True"
    }
  },
  "rules": [
    {
      "apiGroups": ["rbac"],
      "verbs": ["all"]
    }
  ]
}
`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := parseJSONFile([]byte(tc.contents))

			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			} else if err != nil {
				t.Fatal(fmt.Errorf("unexpected error: %w", err))
			}

			if diff := cmp.Diff(tc.expected, actual, cmpopts.EquateEmpty()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
