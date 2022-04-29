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
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/discovery"
	"kpt.dev/configsync/pkg/validate/final"
	"kpt.dev/configsync/pkg/validate/objects"
	"kpt.dev/configsync/pkg/validate/raw"
	"kpt.dev/configsync/pkg/validate/scoped"
	"kpt.dev/configsync/pkg/validate/tree"
)

// VisitorFunc is a function that validates and/or hydrates the given set of
// FileObjects. It enables callers to inject extra validation and hydration
// steps as needed.
type VisitorFunc func(objs []ast.FileObject) ([]ast.FileObject, status.MultiError)

// Options contains the various pieces of information needed by different steps
// in the validation and hydration process.
type Options struct {
	// ClusterName is the spec.clusterName of the cluster's ConfigManagement. This
	// is used when hydrating cluster selectors.
	ClusterName string
	// PolicyDir is the relative path of the root policy directory within the
	// repo.
	PolicyDir cmpath.Relative
	// PreviousCRDs is a list of the CRDs that were declared in the previous set
	// of FileObjects that were validated. This is used to validate that we only
	// remove a CRD if all of its CRs are gone as well.
	PreviousCRDs []*v1beta1.CustomResourceDefinition
	// BuildScoper is a function that builds a Scoper to identify which objects
	// are cluster-scoped or namespace-scoped.
	BuildScoper discovery.BuildScoperFunc
	// Converter is used to encode the declared fields of each object into an
	// annotation on that object so that the validating admission webhook can
	// prevent those fields from being changed.
	Converter *declared.ValueConverter
	// AllowUnknownKinds is a flag to determine if we should throw an error or
	// proceed when the Scoper is unable to determine the scope of an object
	// kind. We only set this to true if a tool is running in offline mode (eg we
	// are running nomos vet without contacting the API server).
	AllowUnknownKinds bool
	// DefaultNamespace is the namespace to assign to namespace-scoped objects
	// which do not specify a namespace in an unstructured repo. Objects in a
	// hierarchical repo are assigned to the namespace that matches their
	// directory.
	DefaultNamespace string
	// IsNamespaceReconciler is a flag to indicate if the caller is a namespace
	// reconciler which adds some additional validation logic.
	IsNamespaceReconciler bool
	// Visitors is a list of optional visitor functions which can be used to
	// inject additional validation or hydration steps on the final objects.
	Visitors []VisitorFunc
}

// Hierarchical validates and hydrates the given FileObjects from a structured,
// hierarchical repo.
func Hierarchical(objs []ast.FileObject, opts Options) ([]ast.FileObject, status.MultiError) {
	// First we perform initial validation which includes:
	//   - checking for illegal metadata or resource kinds
	//   - checking for illegal or invalid directories, namespaces, or names
	//   - validating Config Sync kinds used for cluster selection
	// We also perform initial hydration which includes:
	//   - filtering out resources whose cluster selector does not match
	//   - adding metadata to resources (such as their filepath in the repo)
	rawObjects := &objects.Raw{
		ClusterName:       opts.ClusterName,
		PolicyDir:         opts.PolicyDir,
		Objects:           objs,
		PreviousCRDs:      opts.PreviousCRDs,
		BuildScoper:       opts.BuildScoper,
		Converter:         opts.Converter,
		AllowUnknownKinds: opts.AllowUnknownKinds,
	}

	// nonBlockingErrs tracks the errors which do not block the apply stage
	var nonBlockingErrs status.MultiError
	if errs := raw.Hierarchical(rawObjects); errs != nil {
		if status.HasBlockingErrors(errs) {
			return nil, errs
		}
		nonBlockingErrs = status.Append(nonBlockingErrs, errs)
	}

	// Next we group the objects based upon their scope (cluster vs namespaced)
	// and before the next round of validation on them which includes:
	//   - checking for namespaces being specified on cluster-scoped objects
	//   - checking for namespace selectors on cluster-scoped objects
	scopedObjects, scopeErrs := rawObjects.Scoped()
	if status.HasBlockingErrors(scopeErrs) {
		return nil, status.Append(nonBlockingErrs, scopeErrs)
	}
	nonBlockingErrs = status.Append(nonBlockingErrs, scopeErrs)

	if errs := scoped.Hierarchical(scopedObjects); errs != nil {
		return nil, status.Append(nonBlockingErrs, errs)
	}

	// Now we arrange the namespace-scoped objects into a hierarchical tree based
	// upon their directory structure. Then we perform validation which includes:
	//   - checking for invalid HierarchyConfigs
	//   - checking for invalid directory structure for inheritance and namespace
	//     selection
	// We also perform hydration which includes:
	//   - copying "abstract" resources down into child namespaces and filtering
	//     based upon their namespace selector
	treeObjects, errs := objects.BuildTree(scopedObjects)
	if errs != nil {
		return nil, status.Append(nonBlockingErrs, errs)
	}
	if errs = tree.Hierarchical(treeObjects); errs != nil {
		return nil, status.Append(nonBlockingErrs, errs)
	}

	// We perform a final round of validation on the flattened collection of
	// objects. There is no hydration here so that we can perform validation which
	// depends on the final state of the objects. This includes:
	//   - checking for resources with duplicate GKNNs
	//   - checking for managed resources in unmanaged namespaces
	finalObjects := treeObjects.Objects()
	if errs = final.Validation(finalObjects); errs != nil {
		return nil, status.Append(nonBlockingErrs, errs)
	}

	for _, visitor := range opts.Visitors {
		finalObjects, errs = visitor(finalObjects)
		if errs != nil {
			return nil, status.Append(nonBlockingErrs, errs)
		}
	}

	return finalObjects, nonBlockingErrs
}

// Unstructured validates and hydrates the given FileObjects from an
// unstructured repo.
func Unstructured(objs []ast.FileObject, opts Options) ([]ast.FileObject, status.MultiError) {
	// First we perform initial validation which includes:
	//   - checking for illegal metadata or resource kinds
	//   - checking for illegal or invalid namespaces or names
	//   - validating Config Sync kinds used for cluster selection
	// We also perform initial hydration which includes:
	//   - filtering out resources whose cluster selector does not match
	//   - adding metadata to resources (such as their filepath in the repo)
	rawObjects := &objects.Raw{
		ClusterName:       opts.ClusterName,
		PolicyDir:         opts.PolicyDir,
		Objects:           objs,
		PreviousCRDs:      opts.PreviousCRDs,
		BuildScoper:       opts.BuildScoper,
		Converter:         opts.Converter,
		AllowUnknownKinds: opts.AllowUnknownKinds,
	}

	// nonBlockingErrs tracks the errors which do not block the apply stage
	var nonBlockingErrs status.MultiError
	if errs := raw.Unstructured(rawObjects); errs != nil {
		if status.HasBlockingErrors(errs) {
			return nil, errs
		}
		nonBlockingErrs = status.Append(nonBlockingErrs, errs)
	}

	// Next we group the objects based upon their scope (cluster vs namespaced)
	// and before the next round of validation on them which includes:
	//   - checking for namespaces being specified on cluster-scoped objects
	//   - checking for namespace selectors on cluster-scoped objects
	// We also perform the next round of hydration which includes:
	//   - copy "abstract" resources into zero or more namespaces based upon their
	//     namespace selector
	scopedObjects, scopeErrs := rawObjects.Scoped()
	if status.HasBlockingErrors(scopeErrs) {
		return nil, status.Append(nonBlockingErrs, scopeErrs)
	}
	nonBlockingErrs = status.Append(nonBlockingErrs, scopeErrs)

	scopedObjects.DefaultNamespace = opts.DefaultNamespace
	scopedObjects.IsNamespaceReconciler = opts.IsNamespaceReconciler
	if errs := scoped.Unstructured(scopedObjects); errs != nil {
		return nil, status.Append(nonBlockingErrs, errs)
	}

	// We perform a final round of validation on the flattened collection of
	// objects. There is no hydration here so that we can perform validation which
	// depends on the final state of the objects. This includes:
	//   - checking for resources with duplicate GKNNs
	//   - checking for managed resources in unmanaged namespaces
	finalObjects := scopedObjects.Objects()
	if errs := final.Validation(finalObjects); errs != nil {
		return nil, status.Append(nonBlockingErrs, errs)
	}

	for _, visitor := range opts.Visitors {
		var errs status.MultiError
		finalObjects, errs = visitor(finalObjects)
		if errs != nil {
			return nil, status.Append(nonBlockingErrs, errs)
		}
	}

	return finalObjects, nonBlockingErrs
}
