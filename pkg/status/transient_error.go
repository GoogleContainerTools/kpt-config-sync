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

package status

// TransientErrorCode is the error code for a transient error that might be auto-resolved in the retry.
const TransientErrorCode = "2016"

// transientErrorBuilder is an ErrorBuilder for transient errors.
var transientErrorBuilder = NewErrorBuilder(TransientErrorCode)

// TransientError returns a transient error.
func TransientError(err error) Error {
	return transientErrorBuilder.Wrap(err).Build()
}

// AllTransientErrors returns true if the MultiError is non-nil, non-empty, and
// contains only TransientErrors.
func AllTransientErrors(multiErr MultiError) bool {
	if multiErr == nil {
		return false
	}
	errs := multiErr.Errors()
	for _, err := range errs {
		if err.Code() != TransientErrorCode {
			return false
		}
	}
	// MultiError shouldn't be empty, but check just in case
	return len(errs) > 0
}
