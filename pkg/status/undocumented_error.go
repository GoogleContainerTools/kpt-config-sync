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

// UndocumentedErrorCode is the error code for Undocumented.
const UndocumentedErrorCode = "9999"

// UndocumentedErrorBuilder builds Undocumented errors.
var UndocumentedErrorBuilder = NewErrorBuilder(UndocumentedErrorCode)

// UndocumentedErrorf returns a Undocumented with the string representation of the passed object.
func UndocumentedErrorf(format string, a ...interface{}) Error {
	return UndocumentedErrorBuilder.Sprintf(format, a...).Build()
}

// UndocumentedError returns a Undocumented with the string representation of the passed object.
func UndocumentedError(message string) Error {
	return UndocumentedErrorBuilder.Sprint(message).Build()
}

func undocumented(err error) Error {
	return UndocumentedErrorBuilder.Wrap(err).Build()
}
