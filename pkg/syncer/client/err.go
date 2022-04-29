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

package client

// noUpdateNeededError is returned if no update is needed for the given resource.
type noUpdateNeededError struct {
}

// Error implements error
func (e *noUpdateNeededError) Error() string {
	return "noUpdateNeededError"
}

// NoUpdateNeeded returns an error code for update not required.
func NoUpdateNeeded() error {
	return &noUpdateNeededError{}
}

// isNoUpdateNeeded checks for whether the returned error is noUpdateNeededError
func isNoUpdateNeeded(err error) bool {
	_, ok := err.(*noUpdateNeededError)
	return ok
}
