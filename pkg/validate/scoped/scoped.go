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
	"context"

	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/fileobjects"
	"kpt.dev/configsync/pkg/validate/scoped/hydrate"
	"kpt.dev/configsync/pkg/validate/scoped/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Hierarchical performs the second round of validation and hydration for a
// structured hierarchical repo against the given Scoped objects. Note that this
// will modify the Scoped objects in-place.
func Hierarchical(objs *fileobjects.Scoped) status.MultiError {
	var errs status.MultiError
	// See the note about ordering raw.Hierarchical().
	validators := []fileobjects.ScopedVisitor{
		fileobjects.VisitClusterScoped(validate.ClusterScoped),
		fileobjects.VisitNamespaceScoped(validate.NamespaceScoped),
		validate.NamespaceSelectors,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}

	hydrators := []fileobjects.ScopedVisitor{
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
func Unstructured(ctx context.Context, c client.Client, fieldManager string, objs *fileobjects.Scoped) status.MultiError {
	var errs status.MultiError
	var validateClusterScoped fileobjects.ObjectVisitor
	if objs.Scope == declared.RootScope {
		validateClusterScoped = validate.ClusterScoped
	} else {
		validateClusterScoped = validate.ClusterScopedForNamespaceReconciler
	}

	// See the note about ordering raw.Hierarchical().
	validators := []fileobjects.ScopedVisitor{
		fileobjects.VisitClusterScoped(validateClusterScoped),
		fileobjects.VisitNamespaceScoped(validate.NamespaceScoped),
		validate.NamespaceSelectors,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}
	if errs != nil {
		return errs
	}

	hydrators := []fileobjects.ScopedVisitor{
		hydrate.NamespaceSelectors(ctx, c, fieldManager),
		hydrate.UnknownScope,
	}
	for _, hydrator := range hydrators {
		errs = status.Append(errs, hydrator(objs))
	}
	return errs
}
