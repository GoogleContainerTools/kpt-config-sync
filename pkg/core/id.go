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

package core

import (
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ID uniquely identifies a resource on an API Server.
type ID struct {
	schema.GroupKind
	client.ObjectKey
}

// IDOf converts an Object to its ID.
//
// Panics if the GroupKind is not set and not registered in core.Scheme.
//
// Deprecated: Typed objects should not include GroupKind! Use LookupID instead,
// and handle the error, instead of panicking or returning an ID with empty
// GroupKind.
func IDOf(o client.Object) ID {
	gvk, err := kinds.Lookup(o, Scheme)
	if err != nil {
		klog.Fatalf("IDOf: Failed to lookup GroupKind of %T: %v", o, err)
	}
	return ID{
		GroupKind: gvk.GroupKind(),
		ObjectKey: client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()},
	}
}

// LookupID returns the ID of the Object.
// Returns an error if the GroupKind is not set and not registered in the scheme.
func LookupID(obj client.Object, scheme *runtime.Scheme) (ID, error) {
	gvk, err := kinds.Lookup(obj, scheme)
	if err != nil {
		return ID{}, fmt.Errorf("failed to lookup GroupKind of %T: %w", obj, err)
	}
	return ID{
		GroupKind: gvk.GroupKind(),
		ObjectKey: client.ObjectKeyFromObject(obj),
	}, nil
}

// String implements fmt.Stringer.
func (i ID) String() string {
	return fmt.Sprintf("%s, %s/%s", i.GroupKind.String(), i.Namespace, i.Name)
}

// GKNN is used to set and verify the `configsync.gke.io/resource-id` annotation.
// Changing this function should be avoided, since it may
// introduce incompability across different Config Sync versions.
//
// Panics if the GroupKind is not set and not registered in core.Scheme.
//
// Deprecated: Typed objects should not include GroupKind! Use LookupGKNN instead,
// and handle the error, instead of panicking or returning a GKNN with empty
// GroupKind.
func GKNN(o client.Object) string {
	if o == nil {
		return ""
	}

	gvk, err := kinds.Lookup(o, Scheme)
	if err != nil {
		klog.Fatalf("GKNN: Failed to lookup GroupKind of %T: %v", o, err)
	}

	group := gvk.Group
	kind := gvk.Kind
	if o.GetNamespace() == "" {
		return fmt.Sprintf("%s_%s_%s", group, strings.ToLower(kind), o.GetName())
	}
	return fmt.Sprintf("%s_%s_%s_%s", group, strings.ToLower(kind), o.GetNamespace(), o.GetName())
}

// GKNNs returns the `configsync.gke.io/resource-id` annotations of th given objects as
// a string slice in increasing order.
//
// Panics if the GroupKind is not set and not registered in core.Scheme.
//
// Deprecated: Typed objects should not include GroupKind! Use LookupGKNNs
// instead, and handle the error, instead of panicking or returning a GKNN with
// empty GroupKind.
func GKNNs(objs []client.Object) []string {
	var result []string
	for _, obj := range objs {
		result = append(result, GKNN(obj))
	}
	sort.Strings(result)
	return result
}
