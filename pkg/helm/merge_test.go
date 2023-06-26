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

package helm

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kustomize/kyaml/yaml"
)

func TestMergeTwo(t *testing.T) {
	testCases := map[string]struct {
		file1 string
		file2 string

		expected string
	}{
		"namespaces with lists": {
			file1: `  
namespaces:
- name: foo
  rolebindings:
    name: foo-rb`,
			file2: `  
namespaces:
- name: bar
  rolebindings:
    name: bar-rb`,

			expected: `namespaces:
- name: foo
  rolebindings:
    name: foo-rb
- name: bar
  rolebindings:
    name: bar-rb
`,
		},
		"map overrides": {
			file1: `  
foo: bar-1`,
			file2: `  
foo: bar-2`,
			expected: `foo: bar-1
`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var firstMap map[string]interface{}
			require.NoError(t, yaml.Unmarshal([]byte(tc.file1), &firstMap))
			var secondMap map[string]interface{}
			require.NoError(t, yaml.Unmarshal([]byte(tc.file2), &secondMap))

			actualMap, err := mergeTwo(firstMap, secondMap)
			require.NoError(t, err)

			actual, err := yaml.Marshal(actualMap)
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(actual))
		})
	}
}

func TestMerge(t *testing.T) {
	testCases := map[string]struct {
		file1 string
		file2 string
		file3 string

		expected string
	}{
		"namespaces with lists": { // list elements should get concatenated
			file1: `  
namespaces:
- name: foo
  rolebindings:
    name: foo-rb`,
			file2: `  
namespaces:
- name: bar
  rolebindings:
    name: bar-rb`,
			file3: `  
namespaces:
- name: foobar
  rolebindings:
    name: foobar-rb`,
			expected: `namespaces:
- name: foobar
  rolebindings:
    name: foobar-rb
- name: bar
  rolebindings:
    name: bar-rb
- name: foo
  rolebindings:
    name: foo-rb
`,
		},
		"map overrides": { // last file should take precedence
			file1: `  
abc: def
foo: bar-1
`,
			file2: `  
abc: efg
foo: bar-2`,
			file3: `
foo: bar-3`,
			expected: `abc: efg
foo: bar-3
`,
		},
		"mismatched types": {
			file1: `
abc:
  - def`,
			file2: `
abc: def`,
			file3: `
abc: 
  foo: bar`,

			expected: `abc:
  foo: bar
`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			input := [][]byte{
				[]byte(tc.file1),
				[]byte(tc.file2),
				[]byte(tc.file3),
			}

			actual, err := merge(input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(actual))
		})
	}
}
