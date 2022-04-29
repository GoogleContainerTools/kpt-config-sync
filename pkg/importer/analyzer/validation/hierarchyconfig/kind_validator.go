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

package hierarchyconfig

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UnsupportedResourceInHierarchyConfigErrorCode is the error code for UnsupportedResourceInHierarchyConfigError
const UnsupportedResourceInHierarchyConfigErrorCode = "1041"

var unsupportedResourceInHierarchyConfigError = status.NewErrorBuilder(UnsupportedResourceInHierarchyConfigErrorCode)

// UnsupportedResourceInHierarchyConfigError reports that config management is unsupported for a Resource defined in a HierarchyConfig.
func UnsupportedResourceInHierarchyConfigError(config client.Object, gk schema.GroupKind) status.Error {
	return unsupportedResourceInHierarchyConfigError.
		Sprintf("The %q APIResource MUST NOT be declared in a HierarchyConfig:",
			gk.String()).
		BuildWithResources(config)
}
