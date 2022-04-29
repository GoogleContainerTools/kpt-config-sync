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
	"kpt.dev/configsync/pkg/api/configmanagement/v1/repo"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterSelectorsForHierarchical verifies that all ClusterSelectors have a
// unique name and are under the correct top-level directory. It also verifies
// that no invalid FileObjects are cluster-selected.
func ClusterSelectorsForHierarchical(objs *objects.Raw) status.MultiError {
	return clusterSelectors(objs, true)
}

// ClusterSelectorsForUnstructured verifies that all ClusterSelectors have a
// unique name. It also verifies that no invalid FileObjects are cluster-selected.
func ClusterSelectorsForUnstructured(objs *objects.Raw) status.MultiError {
	return clusterSelectors(objs, false)
}

func clusterSelectors(objs *objects.Raw, checkDir bool) status.MultiError {
	var errs status.MultiError
	clusterGK := kinds.Cluster().GroupKind()
	selectorGK := kinds.ClusterSelector().GroupKind()
	matches := make(map[string][]client.Object)

	for _, obj := range objs.Objects {
		if err := validateClusterSelectorAnnotation(obj); err != nil {
			errs = status.Append(errs, err)
		}

		objGK := obj.GetObjectKind().GroupVersionKind().GroupKind()
		if objGK == clusterGK || objGK == selectorGK {
			if checkDir {
				sourcePath := obj.OSPath()
				dir := cmpath.RelativeSlash(sourcePath).Split()[0]
				if dir != repo.ClusterRegistryDir {
					errs = status.Append(errs, validation.ShouldBeInClusterRegistryError(obj))
				}
			}
			if objGK == selectorGK {
				matches[obj.GetName()] = append(matches[obj.GetName()], obj)
			}
		}
	}

	for name, duplicates := range matches {
		if len(duplicates) > 1 {
			errs = status.Append(errs, nonhierarchical.SelectorMetadataNameCollisionError(selectorGK.Kind, name, duplicates...))
		}
	}

	return errs
}

func validateClusterSelectorAnnotation(obj ast.FileObject) status.Error {
	if !forbidsSelector(obj) {
		return nil
	}

	if _, hasAnnotation := obj.GetAnnotations()[metadata.LegacyClusterSelectorAnnotationKey]; hasAnnotation {
		return nonhierarchical.IllegalClusterSelectorAnnotationError(obj, metadata.LegacyClusterSelectorAnnotationKey)
	}
	if _, hasAnnotation := obj.GetAnnotations()[metadata.ClusterNameSelectorAnnotationKey]; hasAnnotation {
		return nonhierarchical.IllegalClusterSelectorAnnotationError(obj, metadata.ClusterNameSelectorAnnotationKey)
	}
	return nil
}

func forbidsSelector(obj ast.FileObject) bool {
	gk := obj.GetObjectKind().GroupVersionKind().GroupKind()
	return gk == kinds.Cluster().GroupKind() ||
		gk == kinds.ClusterSelector().GroupKind() ||
		gk == kinds.NamespaceSelector().GroupKind()
}
