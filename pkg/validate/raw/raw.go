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

package raw

import (
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/fileobjects"
	"kpt.dev/configsync/pkg/validate/raw/hydrate"
	"kpt.dev/configsync/pkg/validate/raw/validate"
)

// Hierarchical performs initial validation and hydration for a structured
// hierarchical repo against the given Raw objects. Note that this will modify
// the Raw objects in-place.
func Hierarchical(objs *fileobjects.Raw) status.MultiError {
	var errs status.MultiError
	// Note that the ordering here and in all other collections of validators is
	// somewhat arbitrary. We always run all validators in a collection before
	// exiting with any errors. We do put more "fundamental" validation checks
	// first so that their errors will appear first. For example, we check for
	// illegal objects first before we check for an illegal namespace on that
	// object. That way the first error in the list is more likely the real
	// problem (eg they need to remove the object rather than fixing its
	// namespace).
	validators := []fileobjects.RawVisitor{
		fileobjects.VisitAllRaw(validate.Annotations),
		fileobjects.VisitAllRaw(validate.Labels),
		fileobjects.VisitAllRaw(validate.IllegalKindsForHierarchical),
		fileobjects.VisitAllRaw(validate.DeprecatedKinds),
		fileobjects.VisitAllRaw(validate.Name),
		fileobjects.VisitAllRaw(validate.Namespace),
		fileobjects.VisitAllRaw(validate.Directory),
		fileobjects.VisitAllRaw(validate.HNCLabels),
		fileobjects.VisitAllRaw(validate.ManagementAnnotation),
		fileobjects.VisitAllRaw(validate.IllegalCRD),
		fileobjects.VisitAllRaw(validate.CRDName),
		fileobjects.VisitAllRaw(validate.SelfReconcile(declared.ReconcilerNameFromScope(objs.Scope, objs.SyncName))),
		validate.DisallowedFields,
		validate.RemovedCRDs,
		validate.ClusterSelectorsForHierarchical,
		validate.Repo,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}
	if errs != nil {
		return errs
	}

	// First we annotate all objects with their declared fields. It is crucial
	// that we do this step before any other hydration so that we capture the
	// object exactly as it is declared in Git. Next we set missing namespaces on
	// objects in namespace directories since cluster selection relies on
	// namespace if a namespace gets filtered out. Then we perform cluster
	// selection so that we can filter out irrelevant objects before trying to
	// modify them.
	hydrators := []fileobjects.RawVisitor{
		hydrate.DeclaredVersion,
		hydrate.ObjectNamespaces,
		hydrate.ClusterSelectors,
		hydrate.ClusterName,
		hydrate.Filepath,
		hydrate.HNCDepth,
		hydrate.PreventDeletion,
	}
	if objs.WebhookEnabled {
		hydrators = append(hydrators, hydrate.DeclaredFields)
	}
	for _, hydrator := range hydrators {
		errs = status.Append(errs, hydrator(objs))
	}
	return errs
}

// Unstructured performs initial validation and hydration for an unstructured
// repo against the given Raw objects. Note that this will modify the Raw
// objects in-place.
func Unstructured(objs *fileobjects.Raw) status.MultiError {
	var errs status.MultiError
	// See the note about ordering above in Hierarchical().
	validators := []fileobjects.RawVisitor{
		fileobjects.VisitAllRaw(validate.Annotations),
		fileobjects.VisitAllRaw(validate.Labels),
		fileobjects.VisitAllRaw(validate.IllegalKindsForUnstructured),
		fileobjects.VisitAllRaw(validate.DeprecatedKinds),
		fileobjects.VisitAllRaw(validate.Name),
		fileobjects.VisitAllRaw(validate.Namespace),
		fileobjects.VisitAllRaw(validate.ManagementAnnotation),
		fileobjects.VisitAllRaw(validate.IllegalCRD),
		fileobjects.VisitAllRaw(validate.CRDName),
		fileobjects.VisitAllRaw(validate.SelfReconcile(declared.ReconcilerNameFromScope(objs.Scope, objs.SyncName))),
		validate.DisallowedFields,
		validate.RemovedCRDs,
		validate.ClusterSelectorsForUnstructured,
	}
	for _, validator := range validators {
		errs = status.Append(errs, validator(objs))
	}
	if errs != nil {
		return errs
	}

	// First we annotate all objects with their declared fields. It is crucial
	// that we do this step before any other hydration so that we capture the
	// object exactly as it is declared in Git. Then we perform cluster selection
	// so that we can filter out irrelevant objects before trying to modify them.
	hydrators := []fileobjects.RawVisitor{
		hydrate.DeclaredVersion,
		hydrate.ClusterSelectors,
		hydrate.ClusterName,
		hydrate.Filepath,
		hydrate.PreventDeletion,
	}
	if objs.WebhookEnabled {
		hydrators = append(hydrators, hydrate.DeclaredFields)
	}
	for _, hydrator := range hydrators {
		errs = status.Append(errs, hydrator(objs))
	}
	return errs
}
