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

package root

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
)

// getGroupKinds returns GroupKind's from a ResourceGroup's spec
func getGroupKinds(spec v1alpha1.ResourceGroupSpec) map[schema.GroupKind]struct{} {
	if len(spec.Resources) == 0 {
		return nil
	}
	gkSet := make(map[schema.GroupKind]struct{})

	for _, res := range spec.Resources {
		gk := schema.GroupKind{
			Group: res.Group,
			Kind:  res.Kind,
		}
		if _, ok := gkSet[gk]; !ok {
			gkSet[gk] = struct{}{}
		}
	}
	return gkSet
}
