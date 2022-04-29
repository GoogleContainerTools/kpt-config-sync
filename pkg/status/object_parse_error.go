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

// ObjectParseErrorCode is the code for ObjectParseError.
const ObjectParseErrorCode = "1006"

var objectParseError = NewErrorBuilder(ObjectParseErrorCode)

// ObjectParseError reports that an object of known type did not match its
// definition, and so it was read in as an *unstructured.Unstructured.
func ObjectParseError(resource client.Object, err error) Error {
	return objectParseError.Wrap(err).
		Sprintf("The following config could not be parsed as a %v", resource.GetObjectKind().GroupVersionKind()).
		BuildWithResources(resource)
}
