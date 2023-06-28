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

func TestListConcatenateTwo(t *testing.T) {
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
		"nested lists": {
			file1: `
first:
  second:
  - third`,
			file2: `
first:
  second:
  - fourth`,
			expected: `first:
  second:
  - third
  - fourth
`,
		},
		"map overrides and different keys": {
			file1: `  
foo: bar-1
abc:
  def: efg 
cow: moo`,
			file2: `  
foo: bar-2
abc:
  hij: klm
moo: cow`,
			expected: `abc:
  def: efg
cow: moo
foo: bar-1
moo: cow
`,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			var firstMap map[string]interface{}
			require.NoError(t, yaml.Unmarshal([]byte(tc.file1), &firstMap))
			var secondMap map[string]interface{}
			require.NoError(t, yaml.Unmarshal([]byte(tc.file2), &secondMap))

			actualMap, err := listConcatenateTwo(firstMap, secondMap)
			require.NoError(t, err)

			actual, err := yaml.Marshal(actualMap)
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(actual))
		})
	}
}

func TestListConcatenate(t *testing.T) {
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
		"nested lists": {
			file1: `
first:
  second:
  - third`,
			file2: `
first:
  second:
  - fourth`,
			file3: `
first:
  second:
  - fifth`,
			expected: `first:
  second:
  - fifth
  - fourth
  - third
`,
		},
		"map and scalar overrides": { // last file should take precedence
			file1: `  
abc: def
foo: bar-1
a:
  b: c
`,
			file2: `  
abc: efg
foo: bar-2
a:
  d: f`,
			file3: `
foo: bar-3
a:
  g: 
    h: i`,
			expected: `a:
  g:
    h: i
abc: efg
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
		"different keys": {
			file1: `
moo-1: cow-1`,
			file2: `
moo-2: cow-2`,
			file3: `
moo-3: cow-3`,

			expected: `moo-1: cow-1
moo-2: cow-2
moo-3: cow-3
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

			actual, err := listConcatenate(input)
			require.NoError(t, err)
			require.Equal(t, tc.expected, string(actual))
		})
	}
}
