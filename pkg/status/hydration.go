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

// InternalHydrationErrorCode is the error code for an internal Error related to the hydration process.
const InternalHydrationErrorCode = "2015"

// ActionableHydrationErrorCode is the error code for a user actionable Error related to the hydration process.
const ActionableHydrationErrorCode = "1068"

// internalHydrationErrorBuilder is an ErrorBuilder for internal errors related to the hydration process.
var internalHydrationErrorBuilder = NewErrorBuilder(InternalHydrationErrorCode)

// actionableHydrationErrorBuilder is an ErrorBuilder for user actionable errors related to the hydration process.
var actionableHydrationErrorBuilder = NewErrorBuilder(ActionableHydrationErrorCode)

// InternalHydrationError returns an internal error related to the hydration process.
func InternalHydrationError(err error, format string, a ...interface{}) Error {
	return internalHydrationErrorBuilder.Wrap(err).Sprintf(format, a...).Build()
}

// HydrationError returns a hydration error.
func HydrationError(code string, err error) Error {
	if code == ActionableHydrationErrorCode {
		return actionableHydrationErrorBuilder.Wrap(err).Build()
	}
	return internalHydrationErrorBuilder.Wrap(err).Build()
}
