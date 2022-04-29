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
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
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
			name: "empty documents",
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
				fake.UnstructuredObject(kinds.Namespace(), core.Name("shipping")),
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
				fake.UnstructuredObject(kinds.Namespace(), core.Name("shipping"), core.Label("a", "---")),
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
				fake.UnstructuredObject(kinds.Namespace(), core.Name("shipping")),
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
				fake.UnstructuredObject(kinds.Namespace(), core.Name("foo")),
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
				fake.UnstructuredObject(kinds.Namespace(), core.Name("foo"),
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
				fake.UnstructuredObject(kinds.Namespace(), core.Name("foo")),
				fake.UnstructuredObject(kinds.Namespace(), core.Name("bar")),
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
				t.Fatal(errors.Wrap(err, "unexpected error"))
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
				t.Fatal(errors.Wrap(err, "unexpected error"))
			}

			if diff := cmp.Diff(tc.expected, actual, cmpopts.EquateEmpty()); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

func TestParseKptfile(t *testing.T) {
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
			name: "empty documents",
			contents: `

---
# comment
`,
		},
		{
			name: "one document with one Kptfile",
			contents: `apiVersion: kpt.dev/v1
kind: Kptfile
metadata:
  name: package-name
---
# comment
`,
			expected: []*unstructured.Unstructured{
				fake.UnstructuredObject(kinds.KptFile(), core.Name("package-name")),
			},
		}, {
			name: "one document with another type",
			contents: `apiVersion: v1
kind: Namespace
metadata:
  name: shipping
  labels:
    "a": "---"
`,
			expectErr: true,
		},
		{
			name: "two documents - one is Kptfile",
			contents: `apiVersion: v1
kind: Namespace
metadata:
  name: shipping
---
apiVersion: kpt.dev/v1alpha1
kind: Kptfile
metadata:
  name: package-name
packageMetadata:
  shortDescription: This is a description
inventory:
  identifier: some-name
  namespace: foo
  labels:
    sonic: youth
  annotations:
    husker: du
`,
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := parseKptfile([]byte(tc.contents))
			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			} else if err != nil {
				t.Fatal(errors.Wrap(err, "unexpected error"))
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
