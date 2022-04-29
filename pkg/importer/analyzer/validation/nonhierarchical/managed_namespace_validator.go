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

// ManagedResourceInUnmanagedNamespaceErrorCode is the error code for illegal
// managed resources in unmanaged Namespaces.
const ManagedResourceInUnmanagedNamespaceErrorCode = "1056"

var managedResourceInUnmanagedNamespaceError = status.NewErrorBuilder(ManagedResourceInUnmanagedNamespaceErrorCode)

// ManagedResourceInUnmanagedNamespace represents managed resources illegally
// declared in an unmanaged Namespace.
func ManagedResourceInUnmanagedNamespace(namespace string, resources ...client.Object) status.Error {
	return managedResourceInUnmanagedNamespaceError.
		Sprintf("Managed resources must not be declared in unmanaged Namespaces. Namespace %q is is declared unmanaged but contains managed resources. Either remove the managed: disabled annotation from Namespace %q or declare its resources as unmanaged by adding configmanagement.gke.io/managed:disabled annotation.", namespace, namespace).
		BuildWithResources(resources...)
}
