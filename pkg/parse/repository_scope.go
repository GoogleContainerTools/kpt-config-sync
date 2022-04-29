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

package parse

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OptionsForScope returns new Options that have been updated for the given
// Scope.
func OptionsForScope(options validate.Options, scope declared.Scope) validate.Options {
	if scope == declared.RootReconciler {
		options.DefaultNamespace = metav1.NamespaceDefault
		options.IsNamespaceReconciler = false
	} else {
		options.DefaultNamespace = string(scope)
		options.IsNamespaceReconciler = true
		options.Visitors = append(options.Visitors, repositoryScopeVisitor(scope))
	}
	return options
}

// repositoryScopeVisitor ensures all objects in a Namespace Repo are either
// 1) The Namespace for the scope, or
// 2) Namespace-scoped objects that define metadata.namespace matching the scope, or
//      omit metadata.namespace.
func repositoryScopeVisitor(scope declared.Scope) validate.VisitorFunc {
	return func(objs []ast.FileObject) ([]ast.FileObject, status.MultiError) {
		var errs status.MultiError
		for _, obj := range objs {
			// By this point we've validated that there are no cluster-scoped objects
			// in this repo.
			switch obj.GetNamespace() {
			case string(scope):
				// This is what we want, so ignore.
			case "":
				// Missing metadata.namespace, so set it to be the one for this Repo.
				// Otherwise this will invalidly default to the "default" Namespace.
				obj.SetNamespace(string(scope))
			default:
				// There's an object declaring an invalid metadata.namespace, so this is
				// an error.
				errs = status.Append(errs, BadScopeErr(obj, scope))
			}
		}
		return objs, errs
	}
}

// BadScopeErr reports that the passed resource declares a Namespace for a
// different Namespace repository.
func BadScopeErr(resource client.Object, want declared.Scope) status.ResourceError {
	return nonhierarchical.BadScopeErrBuilder.
		Sprintf("Resources in the %q repo must either omit metadata.namespace or declare metadata.namespace=%q", want, want).
		BuildWithResources(resource)
}
