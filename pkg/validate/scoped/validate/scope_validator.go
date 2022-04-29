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

package validate

import (
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterScoped validates the given FileObject as a cluster-scoped resource to
// ensure it does not have a namespace or a namespace selector.
func ClusterScoped(obj ast.FileObject) status.Error {
	if obj.GetNamespace() != "" {
		return nonhierarchical.IllegalNamespaceOnClusterScopedResourceError(&obj)
	}
	if hasNamespaceSelector(obj) {
		return nonhierarchical.IllegalNamespaceSelectorAnnotationError(&obj)
	}
	return nil
}

// ClusterScopedForNamespaceReconciler immediately throws an error for any given
// cluster-scoped resource because a namespace reconciler should only manage
// namespace-scoped resources.
func ClusterScopedForNamespaceReconciler(obj ast.FileObject) status.Error {
	return shouldBeInRootErr(&obj)
}

// NamespaceScoped validates the given FileObject as a namespace-scoped resource
// to ensure it does not have both namespace and namespace selector.
func NamespaceScoped(obj ast.FileObject) status.Error {
	if obj.GetNamespace() != "" && hasNamespaceSelector(obj) {
		return nonhierarchical.NamespaceAndSelectorResourceError(obj)
	}
	return nil
}

func hasNamespaceSelector(obj ast.FileObject) bool {
	_, ok := obj.GetAnnotations()[metadata.NamespaceSelectorAnnotationKey]
	return ok
}

func shouldBeInRootErr(resource client.Object) status.ResourceError {
	return nonhierarchical.BadScopeErrBuilder.
		Sprintf("Resources in namespace Repos must be Namespace-scoped type, but objects of type %v are Cluster-scoped. Move %s to the Root repo.",
			resource.GetObjectKind().GroupVersionKind(), resource.GetName()).
		BuildWithResources(resource)
}
