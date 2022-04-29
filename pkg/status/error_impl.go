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
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

// baseErrorImpl represents a root error around which more complex errors are built.
type baseErrorImpl struct {
	code string
}

var _ Error = baseErrorImpl{}

// Error implements error.
func (e baseErrorImpl) Error() string {
	return format(e)
}

// Is implements Error.
func (e baseErrorImpl) Is(target error) bool {
	// Two errors satisfy errors.Is() if they are both status.Error and have the
	// same KNV code.
	if se, ok := target.(Error); ok {
		return e.Code() == se.Code()
	}
	return false
}

// Code implements Error.
func (e baseErrorImpl) Code() string {
	return e.code
}

// Body implements Error.
func (e baseErrorImpl) Body() string {
	return ""
}

// Errors implements MultiError.
func (e baseErrorImpl) Errors() []Error {
	return []Error{e}
}

// ToCME implements Error.
func (e baseErrorImpl) ToCME() v1.ConfigManagementError {
	return fromError(e)
}

// ToCSE implements Error.
func (e baseErrorImpl) ToCSE() v1beta1.ConfigSyncError {
	return cseFromError(e)
}

// Cause implements causer.
func (e baseErrorImpl) Cause() error {
	return nil
}
