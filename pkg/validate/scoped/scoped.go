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

package scoped

import (
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"kpt.dev/configsync/pkg/validate/scoped/hydrate"
	"kpt.dev/configsync/pkg/validate/scoped/validate"
)

// Hierarchical performs the second round of validation and hydration for a
// structured hierarchical repo against the given Scoped objects. Note that this
// will modify the Scoped objects in-place.
func Hierarchical(objs *objects.Scoped) status.MultiError {
	var errs status.MultiError
	// See the note about ordering raw.Hierarchical().
	validators := []objects.ScopedVisitor{
		objects.VisitClusterScoped(validate.ClusterScoped),
		objects.VisitNamespaceScoped(validate.NamespaceScoped),
		validate.NamespaceSelectors,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}

	hydrators := []objects.ScopedVisitor{
		hydrate.UnknownScope,
	}
	for _, hydrator := range hydrators {
		errs = status.Append(errs, hydrator(objs))
	}
	return errs
}

// Unstructured performs the second round of validation and hydration for an
// unstructured repo against the given Scoped objects. Note that this will
// modify the Scoped objects in-place.
func Unstructured(objs *objects.Scoped) status.MultiError {
	var errs status.MultiError
	var validateClusterScoped objects.ObjectVisitor
	if objs.IsNamespaceReconciler {
		validateClusterScoped = validate.ClusterScopedForNamespaceReconciler
	} else {
		validateClusterScoped = validate.ClusterScoped
	}

	// See the note about ordering raw.Hierarchical().
	validators := []objects.ScopedVisitor{
		objects.VisitClusterScoped(validateClusterScoped),
		objects.VisitNamespaceScoped(validate.NamespaceScoped),
		validate.NamespaceSelectors,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}
	if errs != nil {
		return errs
	}

	hydrators := []objects.ScopedVisitor{
		hydrate.NamespaceSelectors,
		hydrate.UnknownScope,
	}
	for _, hydrator := range hydrators {
		errs = status.Append(errs, hydrator(objs))
	}
	return errs
}
