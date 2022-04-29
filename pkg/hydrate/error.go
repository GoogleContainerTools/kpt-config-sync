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

package hydrate

import "kpt.dev/configsync/pkg/status"

// HydrationError is a wrapper of the error in the hydration process with the error code.
type HydrationError interface {
	// Code is the error code to indicate if it is a user error or an internal error.
	Code() string
	error
}

// ActionableError represents the user actionable hydration error.
type ActionableError struct {
	error
}

// NewActionableError returns the wrapper of the user actionable error.
func NewActionableError(e error) ActionableError {
	return ActionableError{e}
}

// Code returns the user actionable error code.
func (e ActionableError) Code() string {
	return status.ActionableHydrationErrorCode
}

// InternalError represents the internal hydration error.
type InternalError struct {
	error
}

// NewInternalError returns the wrapper of the internal error.
func NewInternalError(e error) InternalError {
	return InternalError{e}
}

// Code returns the internal error code.
func (e InternalError) Code() string {
	return status.InternalHydrationErrorCode
}

// HydrationErrorPayload is the payload of the hydration error in the error file.
type HydrationErrorPayload struct {
	// Code is the error code to indicate if it is a user error or an internal error.
	Code string
	// Error is the message of the hydration error.
	Error string
}
