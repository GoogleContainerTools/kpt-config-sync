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
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DuplicateNames verifies that no objects share the same identifying tuple of:
// Group, Kind, metadata.namespace, metadata.name
func DuplicateNames(objs []ast.FileObject) status.MultiError {
	duplicateMap := make(map[groupKindNamespaceName][]ast.FileObject)
	for _, o := range objs {
		gknn := groupKindNamespaceName{
			group:     o.GetObjectKind().GroupVersionKind().Group,
			kind:      o.GetObjectKind().GroupVersionKind().Kind,
			namespace: o.GetNamespace(),
			name:      o.GetName(),
		}
		duplicateMap[gknn] = append(duplicateMap[gknn], o)
	}

	var errs status.MultiError
	for gknn, duplicates := range duplicateMap {
		if len(duplicates) > 1 {
			rs := idResources(duplicates)
			if gknn.GroupKind() == kinds.Namespace().GroupKind() {
				errs = status.Append(errs, nonhierarchical.NamespaceCollisionError(gknn.name, rs...))
			} else if gknn.namespace == "" {
				errs = status.Append(errs, nonhierarchical.ClusterMetadataNameCollisionError(gknn.GroupKind(), gknn.name, rs...))
			} else {
				errs = status.Append(errs, nonhierarchical.NamespaceMetadataNameCollisionError(gknn.GroupKind(), gknn.namespace, gknn.name, rs...))
			}
		}
	}
	return errs
}

type groupKindNamespaceName struct {
	group     string
	kind      string
	namespace string
	name      string
}

// GroupKind is a convenience method to provide the GroupKind it contains.
func (gknn groupKindNamespaceName) GroupKind() schema.GroupKind {
	return schema.GroupKind{
		Group: gknn.group,
		Kind:  gknn.kind,
	}
}

func idResources(objs []ast.FileObject) []client.Object {
	rs := make([]client.Object, len(objs))
	for i, obj := range objs {
		rs[i] = obj
	}
	return rs
}
