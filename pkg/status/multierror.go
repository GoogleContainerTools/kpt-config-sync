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
	"sort"
	"strings"

	"github.com/pkg/errors"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// MultiError represents a collection of errors.
type MultiError interface {
	error
	Errors() []Error
}

var nonBlockingErrorCodes = map[string]struct{}{
	UnknownKindErrorCode:         {},
	EncodeDeclaredFieldErrorCode: {},
}

// HasBlockingErrors return whether `errs` include any blocking errors.
//
// An error is blocking if it requires the users to do something so that
// Config Sync can sync successfully.
func HasBlockingErrors(errs MultiError) bool {
	if errs == nil {
		return false
	}

	for _, err := range errs.Errors() {
		if _, ok := nonBlockingErrorCodes[err.Code()]; !ok {
			return true
		}
	}
	return false
}

// NonBlockingErrors return the non-blocking errors.
func NonBlockingErrors(errs MultiError) []v1beta1.ConfigSyncError {
	var nonBlockingErrs MultiError
	if errs == nil {
		return []v1beta1.ConfigSyncError{}
	}

	for _, err := range errs.Errors() {
		if _, ok := nonBlockingErrorCodes[err.Code()]; !ok {
			nonBlockingErrs = Append(nonBlockingErrs, err)
		}
	}
	return ToCSE(nonBlockingErrs)
}

// Append adds one or more errors to an existing MultiError.
// If m, err, and errs are nil, returns nil.
//
// Requires at least one error to be passed explicitly to prevent developer mistakes.
// There is no valid reason to call Append with exactly one argument.
//
// If err is a MultiError, appends all contained errors.
func Append(m MultiError, err error, errs ...error) MultiError {
	result := &multiError{}

	switch m.(type) {
	case nil:
		// No errors to begin with.
	case *multiError:
		result.errs = m.Errors()
	default:
		for _, e := range m.Errors() {
			result.add(e)
		}
	}

	result.add(err)
	for _, e := range errs {
		result.add(e)
	}

	if len(result.errs) == 0 {
		return nil
	}
	return result
}

// ToCME converts a MultiError to ConfigManagementError.
func ToCME(m MultiError) []v1.ConfigManagementError {
	var cmes []v1.ConfigManagementError

	if m != nil {
		for _, err := range m.Errors() {
			cmes = append(cmes, err.ToCME())
		}
	}

	return cmes
}

// ToCSE converts a MultiError to ConfigSyncErrors.
func ToCSE(m MultiError) []v1beta1.ConfigSyncError {
	var cses []v1beta1.ConfigSyncError

	if m != nil {
		for _, err := range m.Errors() {
			cses = append(cses, err.ToCSE())
		}
	}

	return cses
}

// sortErrors sorts errs according to their codes and body contents.
func sortErrors(errs []Error) {
	sort.Slice(errs, func(i, j int) bool {
		if errs[i].Code() != errs[j].Code() {
			return errs[i].Code() < errs[j].Code()
		}
		return errs[i].Body() < errs[j].Body()
	})
}

// DeepEqual determines whether two MultiError objects are equal.
// DeepEqual sorts the two MultiError objects before comparing them.
// Two `status.Error` objects are considered equal if they have the same code and body.
func DeepEqual(left, right MultiError) bool {
	if left == nil && right == nil {
		return true
	}

	if left == nil || right == nil {
		return false
	}

	leftErrs := left.Errors()
	rightErrs := right.Errors()

	if len(leftErrs) != len(rightErrs) {
		return false
	}

	sortErrors(leftErrs)
	sortErrors(rightErrs)

	for i := range leftErrs {
		if leftErrs[i].Code() != rightErrs[i].Code() || leftErrs[i].Body() != rightErrs[i].Body() {
			return false
		}
	}
	return true
}

var _ MultiError = (*multiError)(nil)

// MultiError is an error that contains multiple errors.
type multiError struct {
	errs []Error
}

// Add adds error to the builder.
// If the type is known to contain an array of error, adds all of the contained errors.
// If the error is nil, do nothing.
func (m *multiError) add(err error) {
	switch e := err.(type) {
	case nil:
		// No error to add if nil.
	case Error:
		m.errs = append(m.errs, e)
	case MultiError:
		m.errs = append(m.errs, e.Errors()...)
	case utilerrors.Aggregate:
		for _, er := range e.Errors() {
			m.add(er)
		}
	default:
		m.errs = append(m.errs, undocumented(err))
	}
}

// Error implements error
func (m *multiError) Error() string {
	return FormatMultiLine(m)
}

// Errors returns a list of the contained errors
func (m *multiError) Errors() []Error {
	if m == nil || len(m.errs) == 0 {
		return nil
	}
	return m.errs
}

// Is allows *multiError to be compared to other errors.
func (m *multiError) Is(target error) bool {
	other, isMultiError := target.(MultiError)
	if !isMultiError {
		return false
	}
	// We care about _equivalence_, not that the underlying types are identical.
	// If the target MultiError has the same set of error IDs as this one, it is
	// "the same". For example, generally we don't care to check that the resource
	// in a ResourceError actually contains a specific structure; there are tests
	// just for that already. This fuzziness makes writing tests that expect
	// specific errors easier.
	otherErrs := other.Errors()

	if len(m.errs) != len(otherErrs) {
		return false
	}

	// If we ever care about sorting, we should copy the errors into a new slice
	// and sort.
	for i := range m.errs {
		if !errors.Is(m.errs[i], otherErrs[i]) {
			return false
		}
	}
	return true
}

// FormatSingleLine format multi-errors into single-style
// Each error is reformatted as single line and joined with new line
func FormatSingleLine(e error) string {
	if uniqueErrors := PurifyError(e); uniqueErrors != nil {
		allErrors := []string{
			fmt.Sprintf("%d error(s) ", len(uniqueErrors)),
		}
		// adding all errors into allErrors
		for idx, err := range uniqueErrors {
			// format message and remove new lines from each error
			formattedErr := fmt.Sprintf("[%d] %v\n", idx+1, err)
			allErrors = append(allErrors, rmNewlines(formattedErr))
		}
		return strings.Join(allErrors, "\n")
	}
	return ""
}

// FormatMultiLine formats multi-errors into multi-line style
// Errors are joined with two new lines
func FormatMultiLine(e error) string {
	if uniqueErrors := PurifyError(e); uniqueErrors != nil {
		allErrors := []string{
			fmt.Sprintf("%d error(s)\n", len(uniqueErrors)),
		}
		for idx, err := range uniqueErrors {
			allErrors = append(allErrors, fmt.Sprintf("[%d] %v\n", idx+1, err))
		}
		// return error messages joined with two new line.
		return strings.Join(allErrors, "\n\n")
	}
	return ""
}

// PurifyError extracts unique errors, sort and return them as array of string
func PurifyError(e error) []string {
	m := toMultiError(e)

	// mErrs is a slice of Error.
	mErrs := m.Errors()

	if len(mErrs) == 0 {
		return nil
	}

	var msgs []string
	for _, err := range mErrs {
		msgs = append(msgs, err.Error())
	}
	sort.Strings(msgs)

	// Since errors are sorted by message we can eliminate duplicates by comparing the current
	// error message with the previous.
	var uniqueErrors = make([]string, 0)
	for idx, err := range msgs {
		if idx == 0 || msgs[idx-1] != err {
			uniqueErrors = append(uniqueErrors, err)
		}
	}
	return uniqueErrors
}

func rmNewlines(err string) string {
	return strings.ReplaceAll(err, "\n", " ")
}

func toMultiError(e error) MultiError {
	if me, ok := e.(MultiError); ok {
		return me
	}
	var m MultiError
	return Append(m, e)
}
