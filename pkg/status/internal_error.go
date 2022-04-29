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

// InternalErrorCode is the error code for Internal.
const InternalErrorCode = "9998"

// InternalErrorBuilder allows creating complex internal errors.
var InternalErrorBuilder = NewErrorBuilder(InternalErrorCode).Sprint("internal error")

// InternalError represents conditions that should ever happen, but that we
// check for so that we can control how the program terminates when these
// unexpected situations occur.
//
// These errors specifically happen when the code has a bug - as long as
// objects are being used as their contracts require, and as long as they
// follow their contracts, it should not be possible to trigger these.
func InternalError(message string) Error {
	return InternalErrorBuilder.Sprint(message).Build()
}

// InternalErrorf returns an InternalError with a formatted message.
func InternalErrorf(format string, a ...interface{}) Error {
	return InternalErrorBuilder.Sprintf(format, a...).Build()
}

// InternalWrap wraps an error as an internal error.
func InternalWrap(err error) Error {
	return InternalErrorBuilder.Wrap(err).Build()
}
