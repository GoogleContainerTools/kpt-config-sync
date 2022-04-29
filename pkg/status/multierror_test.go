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

package status

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
)

var undocumentedErrFoo = UndocumentedError("foo")
var apiServerErrBar = APIServerError(errors.New("bar"), "qux")
var undocumentedErrBaz = UndocumentedError("baz")

var errFooRaw = errors.New("raw foo")
var errBarRaw = errors.New("raw bar")

func multiLineError() string {
	b := strings.Builder{}
	b.WriteString(fmt.Sprintf("%s\n\n\n", "2 error(s)"))
	b.WriteString(fmt.Sprintf("%s\n\n", "[1] KNV9999: raw bar"))
	b.WriteString(fmt.Sprintf("%s\n\n\n", urlBase+"9999"))
	b.WriteString(fmt.Sprintf("%s\n\n", "[2] KNV9999: raw foo"))
	b.WriteString(fmt.Sprintf("%s\n", urlBase+"9999"))
	return b.String()
}

func singleLineError() string {
	b := strings.Builder{}
	b.WriteString(fmt.Sprintf("%s\n", "1 error(s) "))
	b.WriteString("[1] KNV9999: raw bar  ")
	b.WriteString(urlBase + "9999 ")
	return b.String()
}

func TestHasActionableErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  MultiError
		want bool
	}{
		{
			name: "An empty MultiError",
			err:  nil,
			want: false,
		},
		{
			name: "All the errors are actionable",
			err:  &multiError{errs: []Error{undocumented(errFooRaw), apiServerErrBar}},
			want: true,
		},
		{
			name: "Some of the errors are actionable",
			err:  &multiError{errs: []Error{undocumented(errFooRaw), apiServerErrBar, unknownKindError.Build()}},
			want: true,
		},
		{
			name: "All the errors are non-actionable",
			err:  &multiError{errs: []Error{unknownKindError.Build()}},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := HasBlockingErrors(tc.err)
			if got != tc.want {
				t.Errorf(" HasActionableErrors() got %v; want %v", got, tc.want)
			}
		})
	}
}

func TestAppend(t *testing.T) {
	for _, tc := range []struct {
		name   string
		errors []error
		want   MultiError
	}{
		{
			"build golang errors",
			[]error{errFooRaw, errBarRaw},
			&multiError{errs: []Error{undocumented(errFooRaw), undocumented(errBarRaw)}},
		},
		{
			"build status Errors",
			[]error{undocumentedErrFoo, apiServerErrBar},
			&multiError{errs: []Error{undocumentedErrFoo, apiServerErrBar}},
		},
		{
			"build nil errors",
			[]error{nil, nil},
			nil,
		},
		{
			"build mixed errors",
			[]error{undocumentedErrBaz, nil, errFooRaw},
			&multiError{errs: []Error{undocumentedErrBaz, undocumented(errFooRaw)}},
		},
		{
			"combine MultiErrors",
			[]error{&multiError{[]Error{undocumentedErrFoo, apiServerErrBar}}, &multiError{[]Error{undocumentedErrBaz}}},
			&multiError{[]Error{undocumentedErrFoo, apiServerErrBar, undocumentedErrBaz}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errs MultiError
			for _, err := range tc.errors {
				errs = Append(errs, err)
			}

			switch {
			case tc.want == nil && errs == nil:
				// Nothing to check; successful test.
			case tc.want == nil && errs != nil:
				t.Errorf("got %d errors; want 0 errors", len(errs.Errors()))
				t.Errorf("got %v; want nil", errs)
			case tc.want != nil && errs == nil:
				t.Errorf("got nil; want %v", tc.want)
			case tc.want != nil && errs != nil:
				wantErrorLen := len(tc.want.Errors())
				if len(errs.Errors()) != wantErrorLen {
					t.Errorf("got %d errors; want %d errors", len(errs.Errors()), wantErrorLen)
				}
				if errs.Error() != tc.want.Error() {
					t.Errorf("got %v; want %v", errs, tc.want)
				}
			}
		})
	}
}

func TestFormatError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		multiline bool
		errors    []error
		want      string
	}{
		{
			"build golang errors without new line",
			false,
			[]error{errBarRaw},
			singleLineError(),
		},
		{
			"build golang errors with multi line",
			true,
			[]error{errFooRaw, errBarRaw},
			multiLineError(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errs MultiError
			for _, err := range tc.errors {
				errs = Append(errs, err)
			}
			var got string
			if tc.multiline {
				got = FormatMultiLine(errs)
			} else {
				got = FormatSingleLine(errs)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	var nilMultiError multiError
	for _, tc := range []struct {
		name   string
		errors multiError
	}{
		{"a nil multiError has no errors", nilMultiError},
		{"an empty multiError has no errors", multiError{errs: []Error{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			errs := tc.errors.Errors()
			if errs != nil {
				t.Errorf("multiError.Errors() = %v, want nil", errs)
			}
		})
	}
}

func TestDeepEqual(t *testing.T) {
	var nilErr1, nilErr2 MultiError
	for _, tc := range []struct {
		name  string
		left  MultiError
		right MultiError
		want  bool
	}{
		{
			"two nil errors",
			nilErr1,
			nilErr2,
			true,
		},
		{
			"one nil error, one non-nil error",
			nilErr1,
			&multiError{[]Error{undocumentedErrFoo, apiServerErrBar}},
			false,
		},
		{
			"two empty errors",
			&multiError{},
			&multiError{},
			true,
		},
		{
			"two errors with different lengths",
			&multiError{[]Error{undocumentedErrFoo, apiServerErrBar, undocumentedErrBaz}},
			&multiError{[]Error{undocumentedErrFoo, apiServerErrBar}},
			false,
		},
		{
			"two errors with the same code but different bodies",
			&multiError{[]Error{undocumentedErrFoo}},
			&multiError{[]Error{undocumentedErrBaz}},
			false,
		},
		{
			"two errors with the same error sets in different orders",
			&multiError{[]Error{undocumentedErrFoo, undocumentedErrBaz, apiServerErrBar}},
			&multiError{[]Error{apiServerErrBar, undocumentedErrFoo, undocumentedErrBaz}},
			true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := DeepEqual(tc.left, tc.right)
			if tc.want != got {
				t.Errorf("Is() = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestSortErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		errs []Error
		want []Error
	}{
		{
			"nil error",
			nil,
			nil,
		},
		{
			"three errors in sorted order already",
			[]Error{apiServerErrBar, undocumentedErrFoo, undocumentedErrBaz},
			[]Error{apiServerErrBar, undocumentedErrFoo, undocumentedErrBaz},
		},
		{
			"three errors not in sorted order",
			[]Error{undocumentedErrFoo, undocumentedErrBaz, apiServerErrBar},
			[]Error{apiServerErrBar, undocumentedErrFoo, undocumentedErrBaz},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sortErrors(tc.errs)
			sortIncorrectly := false
			for i := range tc.errs {
				if !errors.Is(tc.errs[i], tc.want[i]) {
					sortIncorrectly = true
				}
			}
			if sortIncorrectly {
				t.Errorf("sortErrors() sorted the errs into %v; \nwant %v", tc.errs, tc.want)
			}
		})
	}
}

func TestIs(t *testing.T) {
	var nilErr *multiError
	for _, tc := range []struct {
		name  string
		left  *multiError
		right *multiError
		want  bool
	}{
		{
			"nil error",
			&multiError{[]Error{undocumentedErrFoo, apiServerErrBar}},
			nilErr,
			false,
		},
		{
			"two empty errors",
			&multiError{},
			&multiError{},
			true,
		},
		{
			"two errors with different lengths",
			&multiError{[]Error{undocumentedErrFoo, apiServerErrBar, undocumentedErrBaz}},
			&multiError{[]Error{undocumentedErrFoo, apiServerErrBar}},
			false,
		},
		{
			"two errors with the same code but different bodies",
			&multiError{[]Error{undocumentedErrFoo}},
			&multiError{[]Error{undocumentedErrBaz}},
			true,
		},
		{
			"two errors with the same error sets in different orders",
			&multiError{[]Error{undocumentedErrFoo, undocumentedErrBaz, apiServerErrBar}},
			&multiError{[]Error{apiServerErrBar, undocumentedErrFoo, undocumentedErrBaz}},
			false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.left.Is(tc.right)
			if tc.want != got {
				t.Errorf("Is() = %v; want %v", got, tc.want)
			}
		})
	}
}
