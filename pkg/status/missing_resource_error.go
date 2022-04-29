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

import "sigs.k8s.io/controller-runtime/pkg/client"

// MissingResourceErrorCode is the error code for a MissingResourceError.
const MissingResourceErrorCode = "2011"

var missingResourceError = NewErrorBuilder(MissingResourceErrorCode)

// MissingResourceWrap returns a MissingResourceError wrapping the given error and Resources.
func MissingResourceWrap(err error, msg string, resources ...client.Object) Error {
	return missingResourceError.
		Sprintf("%s: expected resources were not found:", msg).
		Wrap(err).
		BuildWithResources(resources...)
}
