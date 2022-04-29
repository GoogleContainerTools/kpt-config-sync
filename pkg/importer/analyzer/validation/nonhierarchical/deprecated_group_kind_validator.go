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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeprecatedGroupKindErrorCode is the error code for DeprecatedGroupKindError.
const DeprecatedGroupKindErrorCode = "1050"

var deprecatedGroupKindError = status.NewErrorBuilder(DeprecatedGroupKindErrorCode)

// DeprecatedGroupKindError reports usage of a deprecated version of a specific Group/Kind.
func DeprecatedGroupKindError(resource client.Object, expected schema.GroupVersionKind) status.Error {
	return deprecatedGroupKindError.
		Sprintf("The config is using a deprecated Group and Kind. To fix, set the Group and Kind to %q",
			expected.GroupKind().String()).
		BuildWithResources(resource)
}
