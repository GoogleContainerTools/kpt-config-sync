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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/hierarchyconfig"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

// HierarchyConfig verifies that all HierarchyConfig objects specify valid
// namespace-scoped resource kinds and valid inheritance modes.
func HierarchyConfig(tree *objects.Tree) status.MultiError {
	clusterGKs := make(map[schema.GroupKind]bool)
	for _, obj := range tree.Cluster {
		clusterGKs[obj.GetObjectKind().GroupVersionKind().GroupKind()] = true
	}

	var errs status.MultiError
	for _, obj := range tree.HierarchyConfigs {
		errs = status.Append(errs, validateHC(obj, clusterGKs))
	}
	return errs
}

func validateHC(obj ast.FileObject, clusterGKs map[schema.GroupKind]bool) status.MultiError {
	s, err := obj.Structured()
	if err != nil {
		return err
	}

	var errs status.MultiError
	for _, res := range s.(*v1.HierarchyConfig).Spec.Resources {
		// First validate HierarchyMode.
		switch res.HierarchyMode {
		case v1.HierarchyModeNone, v1.HierarchyModeInherit, v1.HierarchyModeDefault:
		default:
			errs = status.Append(errs, hierarchyconfig.IllegalHierarchyModeError(obj, groupKinds(res)[0], res.HierarchyMode))
		}

		// Then validate resource GroupKinds.
		for _, gk := range groupKinds(res) {
			if unsupportedGK(gk) {
				errs = status.Append(errs, hierarchyconfig.UnsupportedResourceInHierarchyConfigError(obj, gk))
			} else if clusterGKs[gk] {
				errs = status.Append(errs, hierarchyconfig.ClusterScopedResourceInHierarchyConfigError(obj, gk))
			}
		}
	}
	return errs
}

func groupKinds(res v1.HierarchyConfigResource) []schema.GroupKind {
	if len(res.Kinds) == 0 {
		return []schema.GroupKind{{Group: res.Group, Kind: ""}}
	}

	gks := make([]schema.GroupKind, len(res.Kinds))
	for i, kind := range res.Kinds {
		gks[i] = schema.GroupKind{Group: res.Group, Kind: kind}
	}
	return gks
}

func unsupportedGK(gk schema.GroupKind) bool {
	return gk == kinds.Namespace().GroupKind() || gk.Group == configmanagement.GroupName || gk.Kind == ""
}
