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
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/status"
)

// AddResourcesFunc is a function that accepts a Scoper and adds resources to
// it.
type AddResourcesFunc func(*Scoper) status.MultiError

// BuildScoperFunc is a function that builds a new Scoper with the given CRDs
// and attempts verifies it against the given FileObjects.
type BuildScoperFunc func([]*v1beta1.CustomResourceDefinition, []ast.FileObject) (Scoper, status.MultiError)

// ScoperBuilder returns a BuildScoperFunc that incorporates the given
// ServerResourcer (for reading resources from the API server) and other
// optional functions for adding resources.
func ScoperBuilder(sr ServerResourcer, addFuncs ...AddResourcesFunc) BuildScoperFunc {
	return func(crds []*v1beta1.CustomResourceDefinition, objs []ast.FileObject) (Scoper, status.MultiError) {
		// Initialize the scoper with the default set of Kubernetes resources and the
		// declared CRDs.
		scoper := CoreScoper()

		for _, addFunc := range addFuncs {
			if err := addFunc(&scoper); err != nil {
				return scoper, err
			}
		}

		// Always add declared CRDs last since it is possible that the cached API Resources conflicts with declared CRDs.
		// For this edge case, the declared CRD takes precedence as, once synced,
		// the new api-resources.txt will eventually be updated to reflect this change.
		scoper.AddCustomResources(crds)

		// If we don't need to check the API Server because we have all the required
		// type information, or the user has passed --no-api-server-check, don't
		// call the API Server.
		if scoper.HasScopesFor(objs) {
			return scoper, nil
		}

		// Build a new Scoper from the cluster's API resource lists.
		apiScoper, err := APIResourceScoper(sr)
		if err != nil {
			return scoper, err
		}

		// Add the other scoper on top so that we override CRDS on the cluster with
		// declared CRDs.
		apiScoper.AddScoper(scoper)
		return apiScoper, nil
	}
}
