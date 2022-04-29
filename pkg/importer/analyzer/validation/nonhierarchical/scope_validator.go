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
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IllegalNamespaceOnClusterScopedResourceErrorCode represents a cluster-scoped resource illegally
// declaring metadata.namespace.
const IllegalNamespaceOnClusterScopedResourceErrorCode = "1052"

var illegalNamespaceOnClusterScopedResourceErrorBuilder = status.NewErrorBuilder(IllegalNamespaceOnClusterScopedResourceErrorCode)

// IllegalNamespaceOnClusterScopedResourceError reports that a cluster-scoped resource MUST NOT declare metadata.namespace.
func IllegalNamespaceOnClusterScopedResourceError(resource client.Object) status.Error {
	return illegalNamespaceOnClusterScopedResourceErrorBuilder.
		Sprint("cluster-scoped resources MUST NOT declare metadata.namespace").
		BuildWithResources(resource)
}

// MissingNamespaceOnNamespacedResourceErrorCode represents a namespace-scoped resource NOT declaring
// metadata.namespace.
const MissingNamespaceOnNamespacedResourceErrorCode = "1053"

var missingNamespaceOnNamespacedResourceErrorBuilder = status.NewErrorBuilder(MissingNamespaceOnNamespacedResourceErrorCode)

// NamespaceAndSelectorResourceError reports that a namespace-scoped resource illegally declares both metadata.namespace
// and has the namespace-selector annotation.
func NamespaceAndSelectorResourceError(resource client.Object) status.Error {
	return missingNamespaceOnNamespacedResourceErrorBuilder.
		Sprintf("namespace-scoped resources MUST NOT declare both metadata.namespace and "+
			"metadata.annotations.%s", metadata.NamespaceSelectorAnnotationKey).
		BuildWithResources(resource)
}

// MissingNamespaceOnNamespacedResourceError reports a namespace-scoped resource MUST declare metadata.namespace.
// when parsing in non-hierarchical mode.
func MissingNamespaceOnNamespacedResourceError(resource client.Object) status.Error {
	return missingNamespaceOnNamespacedResourceErrorBuilder.
		Sprintf("namespace-scoped resources MUST either declare either metadata.namespace or "+
			"metadata.annotations.%s", metadata.NamespaceSelectorAnnotationKey).
		BuildWithResources(resource)
}

// BadScopeErrCode is the error code indicating that a resource has been
// declared in a Namespace repository that shouldn't be there.
const BadScopeErrCode = "1058"

// BadScopeErrBuilder is an error build for errors related to the object scope errors
var BadScopeErrBuilder = status.NewErrorBuilder(BadScopeErrCode)
