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
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UnmanagedNamespaces verifies that no managed resources are located in
// unmanaged Namespaces.
func UnmanagedNamespaces(objs []ast.FileObject) status.MultiError {
	unmanagedNamespaces := make(map[string][]client.Object)
	for _, obj := range objs {
		if obj.GetObjectKind().GroupVersionKind() != kinds.Namespace() {
			continue
		}
		if isUnmanaged(obj) {
			unmanagedNamespaces[obj.GetName()] = []client.Object{}
		}
	}

	for _, obj := range objs {
		ns := obj.GetNamespace()
		if ns == "" || isUnmanaged(obj) {
			continue
		}
		resources, isInUnmanagedNamespace := unmanagedNamespaces[ns]
		if isInUnmanagedNamespace {
			unmanagedNamespaces[ns] = append(resources, obj)
		}
	}

	var errs status.MultiError
	for ns, resources := range unmanagedNamespaces {
		if len(resources) > 0 {
			errs = status.Append(errs, nonhierarchical.ManagedResourceInUnmanagedNamespace(ns, resources...))
		}
	}
	return errs
}

func isUnmanaged(obj client.Object) bool {
	annotation, hasAnnotation := obj.GetAnnotations()[metadata.ResourceManagementKey]
	return hasAnnotation && annotation == metadata.ResourceManagementDisabled
}
