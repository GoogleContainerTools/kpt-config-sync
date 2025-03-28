// Copyright 2025 Google LLC
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

package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

type mockAnnotated struct {
	annotations map[string]string
}

func (m *mockAnnotated) GetAnnotations() map[string]string {
	return m.annotations
}

func (m *mockAnnotated) SetAnnotations(annotations map[string]string) {
	m.annotations = annotations
}

type mockLabeled struct {
	labels map[string]string
}

func (m *mockLabeled) GetLabels() map[string]string {
	return m.labels
}

func (m *mockLabeled) SetLabels(labels map[string]string) {
	m.labels = labels
}

func TestSetAnnotation(t *testing.T) {
	type args struct {
		key   string
		value string
	}
	tests := []struct {
		name     string
		initial  map[string]string
		args     args
		want     bool
		expected map[string]string
	}{
		{
			name:    "Nil Annotations - New Annotation",
			initial: nil,
			args: args{
				key:   "testKey",
				value: "testValue",
			},
			want: true,
			expected: map[string]string{
				"testKey": "testValue",
			},
		},
		{
			name: "Existing Annotations - New Annotation",
			initial: map[string]string{
				"existingKey": "existingValue",
			},
			args: args{
				key:   "newKey",
				value: "newValue",
			},
			want: true,
			expected: map[string]string{
				"existingKey": "existingValue",
				"newKey":      "newValue",
			},
		},
		{
			name: "Existing Annotations - Update Existing Annotation",
			initial: map[string]string{
				"existingKey": "oldValue",
			},
			args: args{
				key:   "existingKey",
				value: "newValue",
			},
			want: true,
			expected: map[string]string{
				"existingKey": "newValue",
			},
		},
		{
			name: "Existing Annotations - Same Annotation Value",
			initial: map[string]string{
				"sameKey": "sameValue",
			},
			args: args{
				key:   "sameKey",
				value: "sameValue",
			},
			want: false,
			expected: map[string]string{
				"sameKey": "sameValue",
			},
		},
		{
			name: "Existing Annotations - Empty Annotation Key",
			initial: map[string]string{
				"": "existingValue",
			},
			args: args{
				key:   "",
				value: "emptyKeyTest",
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyTest",
			},
		},
		{
			name: "Existing Annotations - Empty Annotation Value",
			initial: map[string]string{
				"existingKey": "existingValue",
			},
			args: args{
				key:   "emptyValueKey",
				value: "",
			},
			want: true,
			expected: map[string]string{
				"existingKey":   "existingValue",
				"emptyValueKey": "",
			},
		},
		{
			name:    "Nil Annotations - Empty Annotation Key",
			initial: nil,
			args: args{
				key:   "",
				value: "emptyKeyTest",
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyTest",
			},
		},
		{
			name:    "Nil Annotations - Empty Annotation Value",
			initial: nil,
			args: args{
				key:   "emptyValueKey",
				value: "",
			},
			want: true,
			expected: map[string]string{
				"emptyValueKey": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockAnnotated{annotations: tt.initial}

			got := SetAnnotation(obj, tt.args.key, tt.args.value)
			require.Equal(t, tt.want, got)
			testutil.AssertEqual(t, tt.expected, obj.GetAnnotations())
		})
	}
}

func TestGetAnnotation(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		name    string
		initial map[string]string
		args    args
		want    string
	}{
		{
			name:    "Nil Annotations - Get Non-existent",
			initial: nil,
			args: args{
				key: "testKey",
			},
			want: "",
		},
		{
			name: "Existing Annotations - Get Existing",
			initial: map[string]string{
				"testKey":   "testValue",
				"otherKey":  "otherValue",
				"":          "emptyKeyVal",
				"valueOnly": "",
			},
			args: args{
				key: "testKey",
			},
			want: "testValue",
		},
		{
			name: "Existing Annotations - Get Non-existent",
			initial: map[string]string{
				"testKey": "testValue",
			},
			args: args{
				key: "nonExistentKey",
			},
			want: "",
		},
		{
			name: "Existing Annotations - Get Empty Key",
			initial: map[string]string{
				"": "emptyKeyVal",
			},
			args: args{
				key: "",
			},
			want: "emptyKeyVal",
		},
		{
			name: "Existing Annotations - Get Key with Empty Value",
			initial: map[string]string{
				"valueOnly": "",
			},
			args: args{
				key: "valueOnly",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockAnnotated{annotations: tt.initial}

			got := GetAnnotation(obj, tt.args.key)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRemoveAnnotations(t *testing.T) {
	type args struct {
		keys []string
	}
	tests := []struct {
		name     string
		initial  map[string]string
		args     args
		want     bool
		expected map[string]string
	}{
		{
			name:    "Empty Annotations - No Removal",
			initial: map[string]string{},
			args: args{
				keys: []string{"key1"},
			},
			want:     false,
			expected: map[string]string{},
		},
		{
			name:    "Nil Annotations - No Removal",
			initial: nil,
			args: args{
				keys: []string{"key1"},
			},
			want:     false,
			expected: nil,
		},
		{
			name: "Remove Single Existing Annotation",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"key1"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name: "Remove Multiple Existing Annotations",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
			args: args{
				keys: []string{"key1", "key3"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name: "Remove Non-existent Annotation",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				keys: []string{"nonExistent"},
			},
			want: false,
			expected: map[string]string{
				"key1": "value1",
			},
		},
		{
			name: "Remove Mix of Existing and Non-existent Annotations",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"key1", "nonExistent"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name: "Remove All Annotations",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"key1", "key2"},
			},
			want:     true,
			expected: map[string]string{},
		},
		{
			name: "Remove Empty Annotation Key",
			initial: map[string]string{
				"":     "emptyKeyVal",
				"key1": "value1",
			},
			args: args{
				keys: []string{""},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
			},
		},
		{
			name: "Remove Multiple - Including Empty Key and Non-existent",
			initial: map[string]string{
				"":     "emptyKeyVal",
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"", "key1", "nonExistent"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name:    "Empty Annotations - Try to Remove Non-existent",
			initial: map[string]string{},
			args: args{
				keys: []string{"nonExistent"},
			},
			want:     false,
			expected: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockAnnotated{annotations: tt.initial}

			got := RemoveAnnotations(obj, tt.args.keys...)
			require.Equal(t, tt.want, got)
			testutil.AssertEqual(t, tt.expected, obj.GetAnnotations())
		})
	}
}

func TestAddAnnotations(t *testing.T) {
	type args struct {
		annotations map[string]string
	}
	tests := []struct {
		name     string
		initial  map[string]string
		args     args
		want     bool
		expected map[string]string
	}{
		{
			name:    "Nil Annotations - Add New",
			initial: nil,
			args: args{
				annotations: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "Existing Annotations - Add New",
			initial: map[string]string{
				"existingKey": "existingValue",
			},
			args: args{
				annotations: map[string]string{
					"newKey1": "newValue1",
					"newKey2": "newValue2",
				},
			},
			want: true,
			expected: map[string]string{
				"existingKey": "existingValue",
				"newKey1":     "newValue1",
				"newKey2":     "newValue2",
			},
		},
		{
			name: "Existing Annotations - Update Existing",
			initial: map[string]string{
				"key1": "oldValue",
				"key2": "value2",
			},
			args: args{
				annotations: map[string]string{
					"key1": "newValue",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "newValue",
				"key2": "value2",
			},
		},
		{
			name: "Existing Annotations - Add Same",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				annotations: map[string]string{
					"key1": "value1",
				},
			},
			want: false,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "Existing Annotations - Add Empty Map",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				annotations: map[string]string{},
			},
			want: false,
			expected: map[string]string{
				"key1": "value1",
			},
		},
		{
			name: "Existing Annotations - Add Mix of New and Same",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				annotations: map[string]string{
					"key2": "value2",
					"key1": "value1",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "Existing Annotations - Add Mix of New and Updated",
			initial: map[string]string{
				"key1": "oldValue",
			},
			args: args{
				annotations: map[string]string{
					"key2": "newValue",
					"key1": "updatedValue",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "updatedValue",
				"key2": "newValue",
			},
		},
		{
			name:    "Nil Annotations - Add Empty Key",
			initial: nil,
			args: args{
				annotations: map[string]string{
					"": "emptyKeyVal",
				},
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyVal",
			},
		},
		{
			name: "Existing Annotations - Add Empty Key",
			initial: map[string]string{
				"": "value1",
			},
			args: args{
				annotations: map[string]string{
					"": "emptyKeyVal",
				},
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyVal",
			},
		},
		{
			name:    "Nil Annotations - Add Empty Value",
			initial: nil,
			args: args{
				annotations: map[string]string{
					"key1": "",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "",
			},
		},
		{
			name: "Existing Annotations - Add Empty Value",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				annotations: map[string]string{
					"key2": "",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
				"key2": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockAnnotated{annotations: tt.initial}

			got := AddAnnotations(obj, tt.args.annotations)
			require.Equal(t, tt.want, got)
			testutil.AssertEqual(t, tt.expected, obj.GetAnnotations())
		})
	}
}

func TestSetLabel(t *testing.T) {
	type args struct {
		key   string
		value string
	}
	tests := []struct {
		name     string
		initial  map[string]string
		args     args
		want     bool
		expected map[string]string
	}{
		{
			name:    "Nil Labels - New Label",
			initial: nil,
			args: args{
				key:   "testKey",
				value: "testValue",
			},
			want: true,
			expected: map[string]string{
				"testKey": "testValue",
			},
		},
		{
			name: "Existing Labels - New Label",
			initial: map[string]string{
				"existingKey": "existingValue",
			},
			args: args{
				key:   "newKey",
				value: "newValue",
			},
			want: true,
			expected: map[string]string{
				"existingKey": "existingValue",
				"newKey":      "newValue",
			},
		},
		{
			name: "Existing Labels - Update Existing Label",
			initial: map[string]string{
				"existingKey": "oldValue",
			},
			args: args{
				key:   "existingKey",
				value: "newValue",
			},
			want: true,
			expected: map[string]string{
				"existingKey": "newValue",
			},
		},
		{
			name: "Existing Labels - Same Label Value",
			initial: map[string]string{
				"sameKey": "sameValue",
			},
			args: args{
				key:   "sameKey",
				value: "sameValue",
			},
			want: false,
			expected: map[string]string{
				"sameKey": "sameValue",
			},
		},
		{
			name: "Existing Labels - Empty Label Key",
			initial: map[string]string{
				"": "existingValue",
			},
			args: args{
				key:   "",
				value: "emptyKeyTest",
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyTest",
			},
		},
		{
			name: "Existing Labels - Empty Label Value",
			initial: map[string]string{
				"existingKey": "existingValue",
			},
			args: args{
				key:   "emptyValueKey",
				value: "",
			},
			want: true,
			expected: map[string]string{
				"existingKey":   "existingValue",
				"emptyValueKey": "",
			},
		},
		{
			name:    "Nil Labels - Empty Label Key",
			initial: nil,
			args: args{
				key:   "",
				value: "emptyKeyTest",
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyTest",
			},
		},
		{
			name:    "Nil Labels - Empty Label Value",
			initial: nil,
			args: args{
				key:   "emptyValueKey",
				value: "",
			},
			want: true,
			expected: map[string]string{
				"emptyValueKey": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockLabeled{labels: tt.initial}

			got := SetLabel(obj, tt.args.key, tt.args.value)
			require.Equal(t, tt.want, got)
			testutil.AssertEqual(t, tt.expected, obj.GetLabels())
		})
	}
}

func TestGetLabel(t *testing.T) {
	type args struct {
		key string
	}
	tests := []struct {
		name    string
		initial map[string]string
		args    args
		want    string
	}{
		{
			name:    "Nil Labels - Get Non-existent",
			initial: nil,
			args: args{
				key: "testKey",
			},
			want: "",
		},
		{
			name: "Existing Labels - Get Existing",
			initial: map[string]string{
				"testKey":   "testValue",
				"otherKey":  "otherValue",
				"":          "emptyKeyVal",
				"valueOnly": "",
			},
			args: args{
				key: "testKey",
			},
			want: "testValue",
		},
		{
			name: "Existing Labels - Get Non-existent",
			initial: map[string]string{
				"testKey": "testValue",
			},
			args: args{
				key: "nonExistentKey",
			},
			want: "",
		},
		{
			name: "Existing Labels - Get Empty Key",
			initial: map[string]string{
				"": "emptyKeyVal",
			},
			args: args{
				key: "",
			},
			want: "emptyKeyVal",
		},
		{
			name: "Existing Labels - Get Key with Empty Value",
			initial: map[string]string{
				"valueOnly": "",
			},
			args: args{
				key: "valueOnly",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockLabeled{labels: tt.initial}

			got := GetLabel(obj, tt.args.key)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestRemoveLabels(t *testing.T) {
	type args struct {
		keys []string
	}
	tests := []struct {
		name     string
		initial  map[string]string
		args     args
		want     bool
		expected map[string]string
	}{
		{
			name:    "Empty Labels - No Removal",
			initial: map[string]string{},
			args: args{
				keys: []string{"key1"},
			},
			want:     false,
			expected: map[string]string{},
		},
		{
			name:    "Nil Labels - No Removal",
			initial: nil,
			args: args{
				keys: []string{"key1"},
			},
			want:     false,
			expected: nil,
		},
		{
			name: "Remove Single Existing Label",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"key1"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name: "Remove Multiple Existing Labels",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
				"key3": "value3",
			},
			args: args{
				keys: []string{"key1", "key3"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name: "Remove Non-existent Label",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				keys: []string{"nonExistent"},
			},
			want: false,
			expected: map[string]string{
				"key1": "value1",
			},
		},
		{
			name: "Remove Mix of Existing and Non-existent Labels",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"key1", "nonExistent"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name: "Remove All Labels",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"key1", "key2"},
			},
			want:     true,
			expected: map[string]string{},
		},
		{
			name: "Remove Empty Label Key",
			initial: map[string]string{
				"":     "emptyKeyVal",
				"key1": "value1",
			},
			args: args{
				keys: []string{""},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
			},
		},
		{
			name: "Remove Multiple - Including Empty Key and Non-existent",
			initial: map[string]string{
				"":     "emptyKeyVal",
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				keys: []string{"", "key1", "nonExistent"},
			},
			want: true,
			expected: map[string]string{
				"key2": "value2",
			},
		},
		{
			name:    "Empty Labels - Try to Remove Non-existent",
			initial: map[string]string{},
			args: args{
				keys: []string{"nonExistent"},
			},
			want:     false,
			expected: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockLabeled{labels: tt.initial}

			got := RemoveLabels(obj, tt.args.keys...)
			require.Equal(t, tt.want, got)
			testutil.AssertEqual(t, tt.expected, obj.GetLabels())
		})
	}
}

func TestAddLabels(t *testing.T) {
	type args struct {
		annotations map[string]string
	}
	tests := []struct {
		name     string
		initial  map[string]string
		args     args
		want     bool
		expected map[string]string
	}{
		{
			name:    "Nil Labels - Add New",
			initial: nil,
			args: args{
				annotations: map[string]string{
					"key1": "value1",
					"key2": "value2",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "Existing Labels - Add New",
			initial: map[string]string{
				"existingKey": "existingValue",
			},
			args: args{
				annotations: map[string]string{
					"newKey1": "newValue1",
					"newKey2": "newValue2",
				},
			},
			want: true,
			expected: map[string]string{
				"existingKey": "existingValue",
				"newKey1":     "newValue1",
				"newKey2":     "newValue2",
			},
		},
		{
			name: "Existing Labels - Update Existing",
			initial: map[string]string{
				"key1": "oldValue",
				"key2": "value2",
			},
			args: args{
				annotations: map[string]string{
					"key1": "newValue",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "newValue",
				"key2": "value2",
			},
		},
		{
			name: "Existing Labels - Add Same",
			initial: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
			args: args{
				annotations: map[string]string{
					"key1": "value1",
				},
			},
			want: false,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "Existing Labels - Add Empty Map",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				annotations: map[string]string{},
			},
			want: false,
			expected: map[string]string{
				"key1": "value1",
			},
		},
		{
			name: "Existing Labels - Add Mix of New and Same",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				annotations: map[string]string{
					"key2": "value2",
					"key1": "value1",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "Existing Labels - Add Mix of New and Updated",
			initial: map[string]string{
				"key1": "oldValue",
			},
			args: args{
				annotations: map[string]string{
					"key2": "newValue",
					"key1": "updatedValue",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "updatedValue",
				"key2": "newValue",
			},
		},
		{
			name:    "Nil Labels - Add Empty Key",
			initial: nil,
			args: args{
				annotations: map[string]string{
					"": "emptyKeyVal",
				},
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyVal",
			},
		},
		{
			name: "Existing Labels - Add Empty Key",
			initial: map[string]string{
				"": "value1",
			},
			args: args{
				annotations: map[string]string{
					"": "emptyKeyVal",
				},
			},
			want: true,
			expected: map[string]string{
				"": "emptyKeyVal",
			},
		},
		{
			name:    "Nil Labels - Add Empty Value",
			initial: nil,
			args: args{
				annotations: map[string]string{
					"key1": "",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "",
			},
		},
		{
			name: "Existing Labels - Add Empty Value",
			initial: map[string]string{
				"key1": "value1",
			},
			args: args{
				annotations: map[string]string{
					"key2": "",
				},
			},
			want: true,
			expected: map[string]string{
				"key1": "value1",
				"key2": "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &mockLabeled{labels: tt.initial}

			got := AddLabels(obj, tt.args.annotations)
			require.Equal(t, tt.want, got)
			testutil.AssertEqual(t, tt.expected, obj.GetLabels())
		})
	}
}
