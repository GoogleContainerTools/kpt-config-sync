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

package watch

import (
	"errors"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMergeListOptions(t *testing.T) {
	testCases := []struct {
		name          string
		a, b          *client.ListOptions
		expectedError error
		expectedOpts  *client.ListOptions
	}{
		{
			a:            nil,
			b:            nil,
			expectedOpts: nil,
		},
		{
			a:            &client.ListOptions{},
			b:            nil,
			expectedOpts: &client.ListOptions{},
		},
		{
			a:            nil,
			b:            &client.ListOptions{},
			expectedOpts: &client.ListOptions{},
		},
		// LabelSelectors
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: nil,
			},
			b: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
			b: &client.ListOptions{
				LabelSelector: nil,
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
			b: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.Nothing(),
			},
			b: &client.ListOptions{
				LabelSelector: labels.Nothing(),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.Nothing(),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
			b: &client.ListOptions{
				LabelSelector: labels.Nothing(),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.Nothing(),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.Nothing(),
			},
			b: &client.ListOptions{
				LabelSelector: labels.Everything(),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.Nothing(),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
			},
			b: &client.ListOptions{},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
			},
		},
		{
			name: "LabelSelectors",
			a:    &client.ListOptions{},
			b: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
			},
			b: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
					// TODO: add LabelSelector de-duping (minor optimization)
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
			},
		},
		{
			name: "LabelSelectors",
			a: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
				),
			},
			b: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
				),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
					newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
				),
			},
		},
		// FieldSelectors
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: nil,
			},
			b: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
			b: &client.ListOptions{
				FieldSelector: nil,
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
			b: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.Everything(),
					// TODO: add ListOptions de-duping (minor optimization)
					fields.Everything(),
				),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.Nothing(),
			},
			b: &client.ListOptions{
				FieldSelector: fields.Nothing(),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.Nothing(),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
			b: &client.ListOptions{
				FieldSelector: fields.Nothing(),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.Nothing(),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.Nothing(),
			},
			b: &client.ListOptions{
				FieldSelector: fields.Everything(),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.Nothing(),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key", "value"),
			},
			b: &client.ListOptions{},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key", "value"),
			},
		},
		{
			name: "FieldSelectors",
			a:    &client.ListOptions{},
			b: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key", "value"),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key", "value"),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key", "value"),
			},
			b: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key", "value"),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.OneTermEqualSelector("key", "value"),
					// TODO: add ListOptions de-duping (minor optimization)
					fields.OneTermEqualSelector("key", "value"),
				),
			},
		},
		{
			name: "FieldSelectors",
			a: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key1", "value1"),
			},
			b: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key2", "value2"),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.OneTermEqualSelector("key1", "value1"),
					fields.OneTermEqualSelector("key2", "value2"),
				),
			},
		},
		// Namespace
		{
			name: "Namespace",
			a: &client.ListOptions{
				Namespace: "",
			},
			b: &client.ListOptions{
				Namespace: "example",
			},
			expectedOpts: &client.ListOptions{
				Namespace: "example",
			},
		},
		{
			name: "Namespace",
			a: &client.ListOptions{
				Namespace: "example",
			},
			b: &client.ListOptions{
				Namespace: "",
			},
			expectedOpts: &client.ListOptions{
				Namespace: "example",
			},
		},
		{
			name: "Namespace",
			a: &client.ListOptions{
				Namespace: "example",
			},
			b: &client.ListOptions{
				Namespace: "example",
			},
			expectedOpts: &client.ListOptions{
				Namespace: "example",
			},
		},
		{
			name: "Namespace",
			a: &client.ListOptions{
				Namespace: "example1",
			},
			b: &client.ListOptions{
				Namespace: "example2",
			},
			expectedError: errors.New("cannot merge two different namespaces: example1 & example2"),
		},
		// Limit
		{
			name: "Limit",
			a: &client.ListOptions{
				Limit: 0,
			},
			b: &client.ListOptions{
				Limit: 10,
			},
			expectedOpts: &client.ListOptions{
				Limit: 10,
			},
		},
		{
			name: "Limit",
			a: &client.ListOptions{
				Limit: 10,
			},
			b: &client.ListOptions{
				Limit: 0,
			},
			expectedOpts: &client.ListOptions{
				Limit: 10,
			},
		},
		{
			name: "Limit",
			a: &client.ListOptions{
				Limit: 10,
			},
			b: &client.ListOptions{
				Limit: 10,
			},
			expectedOpts: &client.ListOptions{
				Limit: 10,
			},
		},
		{
			name: "Limit",
			a: &client.ListOptions{
				Limit: 5,
			},
			b: &client.ListOptions{
				Limit: 10,
			},
			expectedError: errors.New("cannot merge two different limits: 5 & 10"),
		},
		// Continue
		{
			name: "Continue",
			a: &client.ListOptions{
				Continue: "",
			},
			b: &client.ListOptions{
				Continue: "abc123",
			},
			expectedOpts: &client.ListOptions{
				Continue: "abc123",
			},
		},
		{
			name: "Continue",
			a: &client.ListOptions{
				Continue: "abc123",
			},
			b: &client.ListOptions{
				Continue: "",
			},
			expectedOpts: &client.ListOptions{
				Continue: "abc123",
			},
		},
		{
			name: "Continue",
			a: &client.ListOptions{
				Continue: "abc123",
			},
			b: &client.ListOptions{
				Continue: "abc123",
			},
			expectedOpts: &client.ListOptions{
				Continue: "abc123",
			},
		},
		{
			name: "Continue",
			a: &client.ListOptions{
				Continue: "abc123",
			},
			b: &client.ListOptions{
				Continue: "123abc",
			},
			expectedError: errors.New("cannot merge two different continue tokens: abc123 & 123abc"),
		},
		// Raw
		{
			name: "Raw",
			a: &client.ListOptions{
				Raw: nil,
			},
			b: &client.ListOptions{
				Raw: &metav1.ListOptions{},
			},
			expectedOpts: &client.ListOptions{
				Raw: &metav1.ListOptions{},
			},
		},
		{
			name: "Raw",
			a: &client.ListOptions{
				Raw: &metav1.ListOptions{},
			},
			b: &client.ListOptions{
				Raw: nil,
			},
			expectedOpts: &client.ListOptions{
				Raw: &metav1.ListOptions{},
			},
		},
		{
			name: "Raw",
			a: &client.ListOptions{
				Raw: &metav1.ListOptions{
					ResourceVersion: "abc123",
				},
			},
			b: &client.ListOptions{
				Raw: nil,
			},
			expectedOpts: &client.ListOptions{
				Raw: &metav1.ListOptions{
					ResourceVersion: "abc123",
				},
			},
		},
		{
			name: "Raw",
			a: &client.ListOptions{
				Raw: nil,
			},
			b: &client.ListOptions{
				Raw: &metav1.ListOptions{
					ResourceVersion: "abc123",
				},
			},
			expectedOpts: &client.ListOptions{
				Raw: &metav1.ListOptions{
					ResourceVersion: "abc123",
				},
			},
		},
		{
			name: "Raw",
			a: &client.ListOptions{
				Raw: &metav1.ListOptions{
					ResourceVersion: "",
				},
			},
			b: &client.ListOptions{
				Raw: &metav1.ListOptions{
					ResourceVersion: "abc123",
				},
			},
			expectedError: errors.New("not yet implemented: " +
				"merging two different raw ListOptions: " +
				"&ListOptions{LabelSelector:,FieldSelector:,Watch:false,ResourceVersion:,TimeoutSeconds:nil,Limit:0,Continue:,AllowWatchBookmarks:false,ResourceVersionMatch:,SendInitialEvents:nil,} & " +
				"&ListOptions{LabelSelector:,FieldSelector:,Watch:false,ResourceVersion:abc123,TimeoutSeconds:nil,Limit:0,Continue:,AllowWatchBookmarks:false,ResourceVersionMatch:,SendInitialEvents:nil,}"),
		},
		// AllTheThings!
		{
			name: "AllTheThings",
			a: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key1", "value1"),
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
				Namespace: "example",
			},
			b: &client.ListOptions{
				FieldSelector: fields.OneTermEqualSelector("key2", "value2"),
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
				Limit:    10,
				Continue: "123abc",
				Raw: &metav1.ListOptions{
					Watch:                true,
					AllowWatchBookmarks:  true,
					ResourceVersion:      "abc123",
					ResourceVersionMatch: metav1.ResourceVersionMatchExact,
					TimeoutSeconds:       ptr.To(int64(10)),
				},
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.OneTermEqualSelector("key1", "value1"),
					fields.OneTermEqualSelector("key2", "value2"),
				),
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
					// TODO: add LabelSelector de-duping (minor optimization)
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
				Namespace: "example",
				Limit:     10,
				Continue:  "123abc",
				Raw: &metav1.ListOptions{
					Watch:                true,
					AllowWatchBookmarks:  true,
					ResourceVersion:      "abc123",
					ResourceVersionMatch: metav1.ResourceVersionMatchExact,
					TimeoutSeconds:       ptr.To(int64(10)),
				},
			},
		},
	}

	// Compare *hasTerm Selectors by String value
	// (the struct is private and the fields are private)
	hasTermFieldSelectorType := reflect.TypeOf(fields.OneTermEqualSelector("", ""))
	hasTermFieldSelectorComparer := cmp.FilterValues(func(a, b fields.Selector) bool {
		return reflect.TypeOf(a) == hasTermFieldSelectorType &&
			reflect.TypeOf(b) == hasTermFieldSelectorType
	}, cmp.Comparer(func(a, b fields.Selector) bool {
		if a == nil && b == nil {
			return true
		}
		if a == nil || b == nil {
			return false
		}
		return a.String() == b.String()
	}))

	asserter := testutil.NewAsserter(
		hasTermFieldSelectorComparer,
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := MergeListOptions(tc.a, tc.b)
			testerrors.AssertEqual(t, tc.expectedError, err)
			asserter.Equal(t, tc.expectedOpts, result)
		})
	}
}

func TestConvertListOptions(t *testing.T) {
	testCases := []struct {
		name          string
		input         *metav1.ListOptions
		expectedError error
		expectedOpts  *client.ListOptions
	}{
		{
			input:        nil,
			expectedOpts: nil,
		},
		{
			input: &metav1.ListOptions{},
			expectedOpts: &client.ListOptions{
				Raw: &metav1.ListOptions{},
			},
		},
		{
			name: "LabelSelectors",
			input: &metav1.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
					newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
				).String(),
			},
			expectedOpts: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
					newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
				),
				Raw: &metav1.ListOptions{
					LabelSelector: labels.NewSelector().Add(
						newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
						newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
					).String(),
				},
			},
		},
		{
			name: "FieldSelector",
			input: &metav1.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.OneTermEqualSelector("key1", "value1"),
					fields.OneTermEqualSelector("key2", "value2"),
				).String(),
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.OneTermEqualSelector("key1", "value1"),
					fields.OneTermEqualSelector("key2", "value2"),
				),
				Raw: &metav1.ListOptions{
					FieldSelector: fields.AndSelectors(
						fields.OneTermEqualSelector("key1", "value1"),
						fields.OneTermEqualSelector("key2", "value2"),
					).String(),
				},
			},
		},
		{
			name: "Limit",
			input: &metav1.ListOptions{
				Limit: 10,
			},
			expectedOpts: &client.ListOptions{
				Limit: 10,
				Raw: &metav1.ListOptions{
					Limit: 10,
				},
			},
		},
		{
			name: "Continue",
			input: &metav1.ListOptions{
				Continue: "abc123",
			},
			expectedOpts: &client.ListOptions{
				Continue: "abc123",
				Raw: &metav1.ListOptions{
					Continue: "abc123",
				},
			},
		},
		{
			name: "Ignored",
			input: &metav1.ListOptions{
				Watch:                true,
				AllowWatchBookmarks:  true,
				ResourceVersion:      "abc123",
				ResourceVersionMatch: metav1.ResourceVersionMatchExact,
				TimeoutSeconds:       ptr.To(int64(10)),
			},
			expectedOpts: &client.ListOptions{
				Raw: &metav1.ListOptions{
					Watch:                true,
					AllowWatchBookmarks:  true,
					ResourceVersion:      "abc123",
					ResourceVersionMatch: metav1.ResourceVersionMatchExact,
					TimeoutSeconds:       ptr.To(int64(10)),
				},
			},
		},
		{
			name: "AllTheThings",
			input: &metav1.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.OneTermEqualSelector("key1", "value1"),
					fields.OneTermEqualSelector("key2", "value2"),
				).String(),
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
					newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
				).String(),
				Watch:                true,
				AllowWatchBookmarks:  true,
				ResourceVersion:      "abc123",
				ResourceVersionMatch: metav1.ResourceVersionMatchExact,
				TimeoutSeconds:       ptr.To(int64(10)),
				Limit:                10,
				Continue:             "123abc",
			},
			expectedOpts: &client.ListOptions{
				FieldSelector: fields.AndSelectors(
					fields.OneTermEqualSelector("key1", "value1"),
					fields.OneTermEqualSelector("key2", "value2"),
				),
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
					newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
				),
				Limit:    10,
				Continue: "123abc",
				Raw: &metav1.ListOptions{
					FieldSelector: fields.AndSelectors(
						fields.OneTermEqualSelector("key1", "value1"),
						fields.OneTermEqualSelector("key2", "value2"),
					).String(),
					LabelSelector: labels.NewSelector().Add(
						newLabelRequirement(t, "key1", selection.Equals, []string{"value1"}),
						newLabelRequirement(t, "key2", selection.Equals, []string{"value2"}),
					).String(),
					Watch:                true,
					AllowWatchBookmarks:  true,
					ResourceVersion:      "abc123",
					ResourceVersionMatch: metav1.ResourceVersionMatchExact,
					TimeoutSeconds:       ptr.To(int64(10)),
					Limit:                10,
					Continue:             "123abc",
				},
			},
		},
	}

	// Compare *hasTerm Selectors by String value
	// (the struct is private and the fields are private)
	hasTermFieldSelectorType := reflect.TypeOf(fields.OneTermEqualSelector("", ""))
	hasTermFieldSelectorComparer := cmp.FilterValues(func(a, b fields.Selector) bool {
		return reflect.TypeOf(a) == hasTermFieldSelectorType &&
			reflect.TypeOf(b) == hasTermFieldSelectorType
	}, cmp.Comparer(func(a, b fields.Selector) bool {
		if a == nil && b == nil {
			return true
		}
		if a == nil || b == nil {
			return false
		}
		return a.String() == b.String()
	}))

	asserter := testutil.NewAsserter(
		cmpopts.EquateErrors(),
		hasTermFieldSelectorComparer,
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ConvertListOptions(tc.input)
			asserter.Equal(t, tc.expectedError, err)
			asserter.Equal(t, tc.expectedOpts, result)
		})
	}
}

func TestUnrollListOptions(t *testing.T) {
	testCases := []struct {
		name         string
		input        *client.ListOptions
		expectedOpts []client.ListOption
	}{
		{
			input:        nil,
			expectedOpts: nil,
		},
		{
			input:        &client.ListOptions{},
			expectedOpts: nil,
		},
		{
			input: &client.ListOptions{
				LabelSelector: labels.NewSelector().Add(
					newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
				),
				FieldSelector: fields.OneTermEqualSelector("key", "value"),
				Namespace:     "example",
				Limit:         10,
				Continue:      "abc123",
				Raw: &metav1.ListOptions{
					Watch:                true,
					AllowWatchBookmarks:  true,
					ResourceVersion:      "abc123",
					ResourceVersionMatch: metav1.ResourceVersionMatchExact,
					TimeoutSeconds:       ptr.To(int64(10)),
				},
			},
			expectedOpts: []client.ListOption{
				client.MatchingLabelsSelector{
					Selector: labels.NewSelector().Add(
						newLabelRequirement(t, "key", selection.Equals, []string{"value"}),
					),
				},
				client.MatchingFieldsSelector{
					Selector: fields.OneTermEqualSelector("key", "value"),
				},
				client.InNamespace("example"),
				client.Limit(10),
				client.Continue("abc123"),
				&RawListOptions{
					Raw: &metav1.ListOptions{
						Watch:                true,
						AllowWatchBookmarks:  true,
						ResourceVersion:      "abc123",
						ResourceVersionMatch: metav1.ResourceVersionMatchExact,
						TimeoutSeconds:       ptr.To(int64(10)),
					},
				},
			},
		},
	}

	// Compare *hasTerm Selectors by String value
	// (the struct is private and the fields are private)
	hasTermFieldSelectorType := reflect.TypeOf(fields.OneTermEqualSelector("", ""))
	hasTermFieldSelectorComparer := cmp.FilterValues(func(a, b fields.Selector) bool {
		return reflect.TypeOf(a) == hasTermFieldSelectorType &&
			reflect.TypeOf(b) == hasTermFieldSelectorType
	}, cmp.Comparer(func(a, b fields.Selector) bool {
		if a == nil && b == nil {
			return true
		}
		if a == nil || b == nil {
			return false
		}
		return a.String() == b.String()
	}))

	asserter := testutil.NewAsserter(
		cmpopts.EquateErrors(),
		hasTermFieldSelectorComparer,
	)

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := UnrollListOptions(tc.input)
			asserter.Equal(t, tc.expectedOpts, result)
		})
	}
}

func newLabelRequirement(t *testing.T, key string, op selection.Operator, vals []string, opts ...field.PathOption) labels.Requirement {
	t.Helper()
	req, err := labels.NewRequirement(key, op, vals, opts...)
	require.NoError(t, err)
	return *req
}
