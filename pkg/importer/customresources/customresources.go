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

package customresources

import (
	"sort"

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/clusterconfig"
)

// GetCRDs will process all given objects into the resulting list of CRDs.
func GetCRDs(fileObjects []ast.FileObject) ([]*v1beta1.CustomResourceDefinition, status.MultiError) {
	var errs status.MultiError
	crdMap := map[schema.GroupKind]*v1beta1.CustomResourceDefinition{}
	for _, obj := range fileObjects {
		if obj.GetObjectKind().GroupVersionKind().GroupKind() != kinds.CustomResourceDefinition() {
			continue
		}

		crd, err := clusterconfig.AsCRD(obj.Unstructured)
		if err != nil {
			errs = status.Append(errs, err)
			continue
		}
		gk := schema.GroupKind{Group: crd.Spec.Group, Kind: crd.Spec.Names.Kind}
		crdMap[gk] = crd
	}

	var result []*v1beta1.CustomResourceDefinition
	for _, crd := range crdMap {
		result = append(result, crd)
	}
	// Sort to ensure deterministic list order.
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result, errs
}
