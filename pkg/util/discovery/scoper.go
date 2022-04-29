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

package discovery

import (
	"fmt"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ScopeType is the namespace/cluster scope of a particular GroupKind.
type ScopeType string

const (
	// ClusterScope is an object scoped to the cluster.
	ClusterScope = ScopeType("Cluster")
	// NamespaceScope is an object scoped to namespace.
	NamespaceScope = ScopeType("Namespace")
	// UnknownScope means we don't know the scope of the type.
	UnknownScope = ScopeType("Unknown")
)

// Scoper wraps a map from GroupKinds to ScopeType.
type Scoper struct {
	scope map[schema.GroupKind]ScopeType
}

// GetObjectScope implements Scoper
func (s *Scoper) GetObjectScope(o client.Object) (ScopeType, status.Error) {
	scope, err := s.GetGroupKindScope(o.GetObjectKind().GroupVersionKind().GroupKind())
	if err != nil {
		// Make the error specific to the object.
		return scope, status.UnknownObjectKindError(o)
	}
	return scope, nil
}

// GetGroupKindScope returns whether the type is namespace-scoped or cluster-scoped.
// Returns an error if the GroupKind is unknown to the cluster.
func (s *Scoper) GetGroupKindScope(gk schema.GroupKind) (ScopeType, status.Error) {
	if s == nil {
		return UnknownScope, status.InternalError("missing Scoper")
	}
	if scope, hasScope := s.scope[gk]; hasScope && scope != UnknownScope {
		return scope, nil
	}
	// We weren't able to get the scope for this type.
	return UnknownScope, status.UnknownGroupKindError(gk)
}

// HasScopesFor returns true if the Scoper knows the scopes for every type in the
// passed slice of FileObjects.
//
// While this could be made more general, it requests a slice of FileObjects
// as this is a convenience method.
func (s *Scoper) HasScopesFor(objects []ast.FileObject) bool {
	for _, o := range objects {
		if _, exists := s.scope[o.GetObjectKind().GroupVersionKind().GroupKind()]; !exists {
			return false
		}
	}
	return true
}

// AddCustomResources updates Scoper with custom resource metadata from the
// provided CustomResourceDefinitions.  It replaces any existing scopes for the
// GroupKinds of the CRDs.
func (s *Scoper) AddCustomResources(crds []*v1beta1.CustomResourceDefinition) {
	gkss := scopesFromCRDs(crds)
	s.add(gkss)
}

// AddAPIResourceLists adds APIResourceLists retrieved from an API Server to
// the Scoper. It replaces any existing scopes for the GroupKinds of the API
// resources.
func (s *Scoper) AddAPIResourceLists(resourceLists []*metav1.APIResourceList) status.MultiError {
	// Collect all of the server-declared scopes.
	var errs status.MultiError
	var allGKSs []GroupKindScope
	for _, list := range resourceLists {
		gkss, err := toGKSs(list)
		if err != nil {
			errs = status.Append(errs, err)
			continue
		}
		allGKSs = append(allGKSs, gkss...)
	}

	// The resource lists contain an invalid GroupVersion. There isn't a clean
	// way to recover from this.
	//
	// This could be more lenient, e.g. if the type we had an error on isn't
	// actually required, we could ignore it.
	if errs != nil {
		return errs
	}

	// Define scopes for all types on the APIServer for which there are not
	// already known scopes.
	s.add(allGKSs)
	return nil
}

// toGVKSs flattens an APIResourceList to the set of GVKs and their respective ObjectScopes.
func toGKSs(lists ...*metav1.APIResourceList) ([]GroupKindScope, error) {
	var result []GroupKindScope

	for _, list := range lists {
		groupVersion, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			return nil, fmt.Errorf("discovery client returned invalid GroupVersion %q: %w", list.GroupVersion, err)
		}

		for _, resource := range list.APIResources {
			gks := GroupKindScope{
				GroupKind: schema.GroupKind{
					Group: groupVersion.Group,
					Kind:  resource.Kind,
				}}
			if resource.Namespaced {
				gks.ScopeType = NamespaceScope
			} else {
				gks.ScopeType = ClusterScope
			}
			result = append(result, gks)
		}
	}

	return result, nil
}

// AddScoper adds the scopes from the given Scoper into this one. If the two
// Scopers have any overlapping scopes defined, the added ones will overwrite
// the existing ones in this Scope.
func (s *Scoper) AddScoper(other Scoper) {
	for gk, st := range other.scope {
		s.SetGroupKindScope(gk, st)
	}
}

// SetGroupKindScope sets the scope for the passed GroupKind.
func (s *Scoper) SetGroupKindScope(gk schema.GroupKind, scope ScopeType) {
	if s.scope == nil {
		s.scope = make(map[schema.GroupKind]ScopeType)
	}
	s.scope[gk] = scope
}

func (s *Scoper) add(gkss []GroupKindScope) {
	// This will overwrite previously-defined scopes for any of the GroupKinds.
	for _, gks := range gkss {
		s.SetGroupKindScope(gks.GroupKind, gks.ScopeType)
	}
}

// GroupKindScope is a Kubernetes type, and whether it is Namespaced.
type GroupKindScope struct {
	schema.GroupKind
	ScopeType
}

// scopesFromCRDs extracts the scopes declared in all passed CRDs.
func scopesFromCRDs(crds []*v1beta1.CustomResourceDefinition) []GroupKindScope {
	var result []GroupKindScope
	for _, crd := range crds {
		if !isServed(crd) {
			continue
		}

		result = append(result, scopeFromCRD(crd))
	}
	return result
}

// isServed returns true if the CRD declares a version servable by the APIServer.
func isServed(crd *v1beta1.CustomResourceDefinition) bool {
	if crd.Spec.Version != "" {
		return true
	}
	for _, version := range crd.Spec.Versions {
		if version.Served {
			return true
		}
	}
	return false
}

func scopeFromCRD(crd *v1beta1.CustomResourceDefinition) GroupKindScope {
	// CRD Scope defaults to Namespaced
	scope := NamespaceScope
	if crd.Spec.Scope == v1beta1.ClusterScoped {
		scope = ClusterScope
	}

	gk := schema.GroupKind{
		Group: crd.Spec.Group,
		Kind:  crd.Spec.Names.Kind,
	}

	return GroupKindScope{
		GroupKind: gk,
		ScopeType: scope,
	}
}
