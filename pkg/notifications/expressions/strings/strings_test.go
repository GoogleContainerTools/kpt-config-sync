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

package strings

import "testing"

func TestJoin(t *testing.T) {
	testCases := []struct {
		description string
		arr         interface{}
		sep         string
		expected    string
	}{
		{
			description: "join a string array",
			arr:         []string{"foo", "bar"},
			sep:         ",",
			expected:    "foo,bar",
		},
		{
			description: "join an interface array with one string",
			arr:         []interface{}{"foo"},
			sep:         ",",
			expected:    "foo",
		},
		{
			description: "join an interface array with multiple strings and , separator",
			arr:         []interface{}{"foo", "bar", "baz"},
			sep:         ",",
			expected:    "foo,bar,baz",
		},
		{
			description: "join an interface array with multiple strings and | separator",
			arr:         []interface{}{"foo", "bar", "baz"},
			sep:         "|",
			expected:    "foo|bar|baz",
		},
		{
			description: "invalid input type",
			arr:         map[string]interface{}{"foo": "bar"},
			sep:         ",",
			expected:    "",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			output := join(tc.arr, tc.sep)
			if output != tc.expected {
				t.Errorf("want %s, got %s", tc.expected, output)
			}
		})
	}
}

func TestHash(t *testing.T) {
	testCases := []struct {
		description string
		input       string
		expected    string
	}{
		{
			description: "hash an empty string",
			input:       "",
			expected:    "2jmj7l5rSw0yVb_vlWAYkK_YBwk",
		},
		{
			description: "hash a string",
			input:       "foo bar baz",
			expected:    "x1Z-izniQo44v5ySJqxo3kxn3Dk",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			output := hash(tc.input)
			if output != tc.expected {
				t.Errorf("want %s, got %s", tc.expected, output)
			}
		})
	}
}
