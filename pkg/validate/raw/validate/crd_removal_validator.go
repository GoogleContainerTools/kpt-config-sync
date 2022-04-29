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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/importer/customresources"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

// RemovedCRDs verifies that the Raw objects do not remove any CRDs that still
// have CRs using them.
func RemovedCRDs(objs *objects.Raw) status.MultiError {
	current, errs := customresources.GetCRDs(objs.Objects)
	if errs != nil {
		return errs
	}
	removed := removedGroupKinds(objs.PreviousCRDs, current)
	for _, obj := range objs.Objects {
		if removed[obj.GetObjectKind().GroupVersionKind().GroupKind()] {
			errs = status.Append(errs, nonhierarchical.UnsupportedCRDRemovalError(obj))
		}
	}
	return errs
}

type removedGKs map[schema.GroupKind]bool

func removedGroupKinds(previous, current []*v1beta1.CustomResourceDefinition) removedGKs {
	removed := make(map[schema.GroupKind]bool)
	for _, crd := range previous {
		gk := schema.GroupKind{Group: crd.Spec.Group, Kind: crd.Spec.Names.Kind}
		removed[gk] = true
	}
	for _, crd := range current {
		gk := schema.GroupKind{Group: crd.Spec.Group, Kind: crd.Spec.Names.Kind}
		delete(removed, gk)
	}
	return removed
}
