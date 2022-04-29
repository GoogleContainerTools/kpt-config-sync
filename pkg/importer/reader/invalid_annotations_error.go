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

package reader

import (
	"strings"

	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InvalidAnnotationValueErrorCode is the error code for when a value in
// metadata.annotations is not a string.
const InvalidAnnotationValueErrorCode = "1054"

var invalidAnnotationValueErrorBase = status.NewErrorBuilder(InvalidAnnotationValueErrorCode)

// InvalidAnnotationValueError reports that an annotation value is coerced to
// a non-string type.
func InvalidAnnotationValueError(resource client.Object, keys []string) status.ResourceError {
	return invalidAnnotationValueErrorBase.
		Sprintf("Values in metadata.annotations MUST be strings. "+
			`To fix, add quotes around the values. Non-string values for:

metadata.annotations.%s `,
			strings.Join(keys, "\nmetadata.annotations.")).
		BuildWithResources(resource)
}
