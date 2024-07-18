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

	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// FakeMultiError returns a MultiError consisting of fake errors. For use in unit tests
// where multiple errors are expected to be returned.
//
// In all cases where a single error is expected, it is safe to use fake.FakeError
// instead.
func FakeMultiError(codes ...string) MultiError {
	var result MultiError
	for i, code := range codes {
		result = Append(result, fakeError{id: i + 1, code: code})
	}
	return result
}

// FakeError returns a fake error for use in tests which matches errors with the
// specified KNV code. This is preferable to requiring test authors to specify
// fields they don't really care about.
func FakeError(code string) Error {
	return fakeError{id: 1, code: code}
}

type fakeError struct {
	id   int
	code string
}

// Cause implements Error.
func (f fakeError) Cause() error {
	return nil
}

// Cause implements Error.
func (f fakeError) Error() string {
	return fmt.Sprintf("KNV%s fake error %d", f.code, f.id)
}

// Errors implements Error.
func (f fakeError) Errors() []Error {
	return []Error{f}
}

// ToCME implements Error.
func (f fakeError) ToCME() v1.ConfigManagementError {
	return v1.ConfigManagementError{
		Code:         f.code,
		ErrorMessage: fmt.Sprintf("fake error %d", f.id),
	}
}

// ToCSE implements Error.
func (f fakeError) ToCSE() v1beta1.ConfigSyncError {
	return v1beta1.ConfigSyncError{
		Code:         f.code,
		ErrorMessage: fmt.Sprintf("fake error %d", f.id),
	}
}

// Code implements Error.
func (f fakeError) Code() string {
	return f.code
}

// Body implements Error.
func (f fakeError) Body() string {
	return f.Error()
}

// Is implements Error.
func (f fakeError) Is(target error) bool {
	switch err := target.(type) {
	case Error:
		return err.Code() == f.code
	case MultiError:
		return len(err.Errors()) == 1 && err.Errors()[0].Code() == f.code
	default:
		return false
	}
}
