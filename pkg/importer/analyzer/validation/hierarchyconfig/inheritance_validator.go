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
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalHierarchyModeErrorCode is the error code for IllegalHierarchyModeError
const IllegalHierarchyModeErrorCode = "1042"

var illegalHierarchyModeError = status.NewErrorBuilder(IllegalHierarchyModeErrorCode)

// IllegalHierarchyModeError reports that a HierarchyConfig is defined with a disallowed hierarchyMode.
func IllegalHierarchyModeError(config client.Object, gk schema.GroupKind, mode v1.HierarchyModeType) status.Error {
	allowedStr := []string{string(v1.HierarchyModeNone), string(v1.HierarchyModeInherit)}
	return illegalHierarchyModeError.Sprintf(
		"HierarchyMode %q is not a valid value for the APIResource %q. Allowed values are [%s].",
		mode, gk.String(), strings.Join(allowedStr, ",")).BuildWithResources(config)
}
