// Copyright 2023 Google LLC
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

package kinds

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ListSuffix is the suffix expected for all Kubernetes collection resources.
const ListSuffix = "List"

// IsListGVK returns true if the kind has a "List" suffix.
func IsListGVK(gvk schema.GroupVersionKind) bool {
	return strings.HasSuffix(gvk.Kind, ListSuffix)
}

// ListGVKForItemGVK returns the item GroupVersionKind with "List" appended to
// the kind.
func ListGVKForItemGVK(gvk schema.GroupVersionKind) schema.GroupVersionKind {
	gvk.Kind += ListSuffix
	return gvk
}

// ItemGVKForListGVK returns the list GroupVersionKind with "List" removed from
// the suffix of the kind.
func ItemGVKForListGVK(gvk schema.GroupVersionKind) schema.GroupVersionKind {
	gvk.Kind = strings.TrimSuffix(gvk.Kind, ListSuffix)
	return gvk
}

// NewTypedListForItemGVK creates a new client.ObjectList using the list type
// registered to the scheme for the specified item GroupVersionKind with "List"
// appended to the kind.
func NewTypedListForItemGVK(itemGVK schema.GroupVersionKind, scheme *runtime.Scheme) (client.ObjectList, error) {
	rObj, err := NewObjectForGVK(ListGVKForItemGVK(itemGVK), scheme)
	if err != nil {
		return nil, err
	}
	return ObjectAsClientObjectList(rObj)
}

// NewUnstructuredListForItemGVK creates a new UnstructuredList using the
// specified item GroupVersionKind with "List" appended to the kind.
func NewUnstructuredListForItemGVK(itemGVK schema.GroupVersionKind) *unstructured.UnstructuredList {
	uList := &unstructured.UnstructuredList{}
	uList.SetGroupVersionKind(ListGVKForItemGVK(itemGVK))
	return uList
}
