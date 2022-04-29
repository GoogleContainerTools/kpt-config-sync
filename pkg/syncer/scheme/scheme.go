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

package scheme

import (
	"reflect"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AddToSchemeAsUnstructured adds the GroupVersionKinds to the scheme as unstructured.Unstructured objects.
func AddToSchemeAsUnstructured(scheme *runtime.Scheme, gvks map[schema.GroupVersionKind]bool) {
	for gvk := range gvks {
		if !scheme.Recognizes(gvk) {
			scheme.AddKnownTypeWithName(gvk, &unstructured.Unstructured{})
			gvkList := schema.GroupVersionKind{
				Group:   gvk.Group,
				Version: gvk.Version,
				Kind:    gvk.Kind + "List",
			}
			scheme.AddKnownTypeWithName(gvkList, &unstructured.UnstructuredList{})
			metav1.AddToGroupVersion(scheme, gvk.GroupVersion())
		}
	}
}

// resourceTypes returns all the sync enabled resources and the corresponding type stored in the scheme.
func resourceTypes(
	gvks map[schema.GroupVersionKind]bool,
	scheme *runtime.Scheme,
) (map[schema.GroupVersionKind]client.Object, error) {
	knownGVKs := scheme.AllKnownTypes()
	m := make(map[schema.GroupVersionKind]client.Object)
	for gvk := range gvks {
		rt, ok := knownGVKs[gvk]
		if !ok {
			return nil, errors.Errorf("trying to sync %q, which hasn't been registered in the scheme", gvk)
		}

		// If it's a resource with an unknown type at compile time, we need to specifically set the GroupVersionKind for it
		// when enabling the watch.
		if rt.AssignableTo(reflect.TypeOf(unstructured.Unstructured{})) {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(gvk)
			m[gvk] = u
		} else {
			m[gvk] = reflect.New(rt).Interface().(client.Object)
		}
	}
	return m, nil
}

// ResourceScopes returns two slices representing the namespace and cluster scoped resource types with sync enabled.
func ResourceScopes(
	gvks map[schema.GroupVersionKind]bool,
	scheme *runtime.Scheme,
	scoper discovery.Scoper,
) (map[schema.GroupVersionKind]client.Object, map[schema.GroupVersionKind]client.Object, error) {
	rts, err := resourceTypes(gvks, scheme)
	if err != nil {
		return nil, nil, err
	}
	namespace := make(map[schema.GroupVersionKind]client.Object)
	cluster := make(map[schema.GroupVersionKind]client.Object)
	for gvk, obj := range rts {
		if gvk.GroupKind() == kinds.CustomResourceDefinition() {
			// CRDs are handled in the CRD controller and shouldn't be handled in any of SubManager's controllers.
			continue
		}

		scope, err := scoper.GetGroupKindScope(gvk.GroupKind())
		switch {
		case scope == discovery.NamespaceScope && err == nil:
			namespace[gvk] = obj
		case scope == discovery.ClusterScope && err == nil:
			cluster[gvk] = obj
		default:
			// The scope is Unknown or there was an error getting the scope.
			return nil, nil, err
		}
	}
	return namespace, cluster, nil
}
