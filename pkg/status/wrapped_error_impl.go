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
	"strings"

	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

type wrappedErrorImpl struct {
	underlying Error
	wrapped    error
}

var _ Error = wrappedErrorImpl{}

// Error implements error.
func (w wrappedErrorImpl) Error() string {
	return format(w)
}

// Is implements Error.
func (w wrappedErrorImpl) Is(target error) bool {
	return w.underlying.Is(target)
}

// Code implements Error.
func (w wrappedErrorImpl) Code() string {
	return w.underlying.Code()
}

// Body implements Error.
func (w wrappedErrorImpl) Body() string {
	var sb strings.Builder
	body := w.underlying.Body()
	wrapped := w.wrapped.Error()
	sb.WriteString(w.underlying.Body())
	if body != "" && wrapped != "" {
		sb.WriteString(": ")
	}
	sb.WriteString(w.wrapped.Error())
	return sb.String()
}

// Errors implements MultiError.
func (w wrappedErrorImpl) Errors() []Error {
	return []Error{w}
}

// ToCME implements Error.
func (w wrappedErrorImpl) ToCME() v1.ConfigManagementError {
	return fromError(w)
}

// ToCSE implements Error.
func (w wrappedErrorImpl) ToCSE() v1beta1.ConfigSyncError {
	return cseFromError(w)
}

// Cause implements causer.
func (w wrappedErrorImpl) Cause() error {
	return w.wrapped
}
