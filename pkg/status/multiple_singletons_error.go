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

// MultipleSingletonsErrorCode is the error code for MultipleSingletonsError
const MultipleSingletonsErrorCode = "2012"

var multipleSingletonsError = NewErrorBuilder(MultipleSingletonsErrorCode)

// MultipleSingletonsError reports that multiple singleton resources were found on the cluster.
func MultipleSingletonsError(duplicates ...client.Object) Error {
	return multipleSingletonsError.Sprintf(
		"Unsupported number of %s resource found: %d, want: 1.", resourceName(duplicates), len(duplicates)).BuildWithResources(duplicates...)
}

func resourceName(dups []client.Object) string {
	if len(dups) == 0 {
		return "singleton"
	}
	return dups[0].GetObjectKind().GroupVersionKind().GroupKind().String()
}
