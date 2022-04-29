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
	"k8s.io/apimachinery/pkg/util/validation"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

// Namespace verifies that the given FileObject has a valid namespace according
// to the following rules:
// - if the object is a Namespace, it must not be `config-management-system`
// - if the object has a metadata namespace, it must be a valid k8s namespace
//   and it must not be in `config-management-system`
func Namespace(obj ast.FileObject) status.Error {
	if obj.GetObjectKind().GroupVersionKind().GroupKind() == kinds.Namespace().GroupKind() {
		return validateNamespace(obj)
	}
	return validateObjectNamespace(obj)
}

func validateNamespace(obj ast.FileObject) status.Error {
	if !isValidNamespace(obj.GetName()) {
		return nonhierarchical.InvalidNamespaceError(obj)
	}
	if configmanagement.IsControllerNamespace(obj.GetName()) {
		return nonhierarchical.IllegalNamespace(obj)
	}
	return nil
}

func validateObjectNamespace(obj ast.FileObject) status.Error {
	ns := obj.GetNamespace()
	if ns == "" {
		return nil
	}
	if !isValidNamespace(ns) {
		return nonhierarchical.InvalidNamespaceError(obj)
	}
	if configmanagement.IsControllerNamespace(ns) &&
		obj.GetKind() != kinds.RootSyncV1Beta1().Kind {
		return nonhierarchical.ObjectInIllegalNamespace(obj)
	}
	return nil
}

// isValidNamespace returns true if Kubernetes allows Namespaces with the name "name".
func isValidNamespace(name string) bool {
	// IsDNS1123Label is misleading as the Kubernetes requirements are more stringent than the specification.
	errs := validation.IsDNS1123Label(name)
	return len(errs) == 0
}
