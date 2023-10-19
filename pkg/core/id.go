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

	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ID uniquely identifies a resource on an API Server.
type ID struct {
	schema.GroupKind
	client.ObjectKey
}

// IDOf converts an Object to its ID.
func IDOf(o client.Object) ID {
	return ID{
		GroupKind: o.GetObjectKind().GroupVersionKind().GroupKind(),
		ObjectKey: client.ObjectKey{Namespace: o.GetNamespace(), Name: o.GetName()},
	}
}

// String implements fmt.Stringer.
func (i ID) String() string {
	return fmt.Sprintf("%s, %s/%s", i.GroupKind.String(), i.Namespace, i.Name)
}

// FromID returns a GKNN representing the provided ID string.
func FromID(id string) (ID, error) {
	parts := strings.Split(id, ",")
	if len(parts) != 2 {
		return ID{}, fmt.Errorf("invalid ID format: %q", id)
	}
	gk := schema.ParseGroupKind(parts[0])
	nn := strings.TrimSpace(parts[1])
	parts = strings.Split(nn, "/")
	if len(parts) != 2 {
		return ID{}, fmt.Errorf("invalid namespace and name format: %q", nn)
	}
	return ID{
		GroupKind: gk,
		ObjectKey: client.ObjectKey{
			Namespace: parts[0],
			Name:      parts[1],
		},
	}, nil
}

// GKNN is used to set and verify the `configsync.gke.io/resource-id` annotation.
// Changing this function should be avoided, since it may
// introduce incompability across different Config Sync versions.
func GKNN(o client.Object) string {
	if o == nil {
		return ""
	}

	group := o.GetObjectKind().GroupVersionKind().Group
	kind := o.GetObjectKind().GroupVersionKind().Kind
	if o.GetNamespace() == "" {
		return fmt.Sprintf("%s_%s_%s", group, strings.ToLower(kind), o.GetName())
	}
	return fmt.Sprintf("%s_%s_%s_%s", group, strings.ToLower(kind), o.GetNamespace(), o.GetName())
}

// GKNNs returns the `configsync.gke.io/resource-id` annotations of th given objects as
// a string slice in increasing order.
func GKNNs(objs []client.Object) []string {
	var result []string
	for _, obj := range objs {
		result = append(result, GKNN(obj))
	}
	sort.Strings(result)
	return result
}

// GVKNN adds Version to core.ID to make it suitable for getting an object from
// a cluster into an *unstructured.Unstructured.
type GVKNN struct {
	ID
	Version string
}

// GroupVersionKind returns the GVK contained in this GVKNN.
func (gvknn GVKNN) GroupVersionKind() schema.GroupVersionKind {
	return gvknn.GroupKind.WithVersion(gvknn.Version)
}

// GVKNNOf converts an Object to its GVKNN.
func GVKNNOf(obj client.Object) GVKNN {
	return GVKNN{
		ID:      IDOf(obj),
		Version: obj.GetObjectKind().GroupVersionKind().Version,
	}
}
