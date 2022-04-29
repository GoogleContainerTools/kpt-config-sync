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

type messageErrorImpl struct {
	underlying Error
	message    string
}

var _ Error = messageErrorImpl{}

// Error implements error.
func (m messageErrorImpl) Error() string {
	return format(m)
}

// Is implements Error.
func (m messageErrorImpl) Is(target error) bool {
	return m.underlying.Is(target)
}

// Code implements Error.
func (m messageErrorImpl) Code() string {
	return m.underlying.Code()
}

// Body implements Error.
func (m messageErrorImpl) Body() string {
	return formatBody(m.message, ": ", m.underlying.Body())
}

// Errors implements MultiError.
func (m messageErrorImpl) Errors() []Error {
	return []Error{m}
}

// ToCME implements Error.
func (m messageErrorImpl) ToCME() v1.ConfigManagementError {
	return fromError(m)
}

// ToCSE implements Error.
func (m messageErrorImpl) ToCSE() v1beta1.ConfigSyncError {
	return cseFromError(m)
}

// Cause implements causer.
func (m messageErrorImpl) Cause() error {
	return m.underlying.Cause()
}
