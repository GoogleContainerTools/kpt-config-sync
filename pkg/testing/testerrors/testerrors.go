// Copyright 2024 Google LLC
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

package testerrors

import (
	"testing"

	"sigs.k8s.io/cli-utils/pkg/testutil"
)

// AssertEqual fails the test if the actual error does not match the expected
// error. Similar to sigs.k8s.io/cli-utils/pkg/testutil.AssertEqual, but
// automatically wraps non-nil expected errors with testutil.EqualError to allow
// matching by type and string value.
//
// This works around an issue with status.Error that breaks errors.Is(), which
// only compares the error code. Using testerrors.Equal, the assertion is more
// strict.
func AssertEqual(t *testing.T, expected, actual error, msgAndArgs ...interface{}) {
	testutil.AssertEqual(t, testableError(expected), actual, msgAndArgs...)
}

func testableError(err error) error {
	if err == nil {
		return nil
	}
	return testutil.EqualError(err)
}
