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
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalNamespaceErrorCode is the error code for illegal Namespace definitions.
const IllegalNamespaceErrorCode = "1034"

var illegalNamespaceError = status.NewErrorBuilder(IllegalNamespaceErrorCode)

// ObjectInIllegalNamespace reports that an object has been declared in an illegal Namespace.
func ObjectInIllegalNamespace(resource client.Object) status.Error {
	return illegalNamespaceError.
		Sprintf("Only %s configs are allowed in the %q namespace",
			kinds.RootSyncV1Beta1().Kind, configmanagement.ControllerNamespace).
		BuildWithResources(resource)
}

// IllegalNamespace reports that the config-management-system Namespace MUST NOT be declared.
func IllegalNamespace(resource client.Object) status.Error {
	return illegalNamespaceError.
		Sprintf("The %q Namespace must not be declared", configmanagement.ControllerNamespace).
		BuildWithResources(resource)
}
