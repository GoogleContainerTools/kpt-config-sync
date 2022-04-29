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
