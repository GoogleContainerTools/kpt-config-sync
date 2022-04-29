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

package nonhierarchical

import (
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalHierarchicalKindErrorCode is the error code for illegalHierarchicalKindErrors.
const IllegalHierarchicalKindErrorCode = "1032"

var illegalHierarchicalKindError = status.NewErrorBuilder(IllegalHierarchicalKindErrorCode)

// IllegalHierarchicalKind reports that a type is not permitted if hierarchical parsing is disabled.
func IllegalHierarchicalKind(resource client.Object) status.Error {
	return illegalHierarchicalKindError.
		Sprintf("The type %v is not allowed if `sourceFormat` is set to "+
			"`unstructured`. To fix, remove the problematic config, or convert your repo "+
			"to use `sourceFormat: hierarchy`.", resource.GetObjectKind().GroupVersionKind().GroupKind().String()).
		BuildWithResources(resource)
}
