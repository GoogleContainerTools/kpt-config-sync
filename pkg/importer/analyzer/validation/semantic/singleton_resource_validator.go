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

package semantic

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TODO: Replace usage of this error with id.MultipleSingletonsError instead

// MultipleSingletonsErrorCode is the error code for MultipleSingletonsError
const MultipleSingletonsErrorCode = "1030"

var multipleSingletonsError = status.NewErrorBuilder(MultipleSingletonsErrorCode)

// MultipleSingletonsError reports that multiple singletons are defined in the same directory.
func MultipleSingletonsError(duplicates ...client.Object) status.Error {
	var gvk schema.GroupVersionKind
	if len(duplicates) > 0 {
		gvk = duplicates[0].GetObjectKind().GroupVersionKind()
	}

	return multipleSingletonsError.
		Sprintf("Multiple %v resources cannot exist in the same directory. "+
			"To fix, remove the duplicate config(s) such that no more than 1 remains:", gvk.GroupKind().String()).
		BuildWithResources(duplicates...)
}
