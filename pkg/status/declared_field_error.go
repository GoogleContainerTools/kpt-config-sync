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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EncodeDeclaredFieldErrorCode is the error code for errors that
// happen when encoding the declared fields.
const EncodeDeclaredFieldErrorCode = "1067"

var encodeDeclaredFieldError = NewErrorBuilder(EncodeDeclaredFieldErrorCode)

// EncodeDeclaredFieldError reports that an error happens when
// encoding the declared fields for an object.
func EncodeDeclaredFieldError(resource client.Object, err error) Error {
	return encodeDeclaredFieldError.Wrap(err).
		Sprintf("failed to encode declared fields").
		BuildWithResources(resource)
}
