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

package differ

import (
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ManagementEnabled returns true if the resource explicitly has management enabled on a resource
// on the API server.
//
// A resource whose `configmanagement.gke.io/managed` anntation is `enabled` may not be
// managed by Config Sync, because the annotation may be copied from another resource
// managed by Config Sync.
//
// Use `ManagedByConfigSync` to decide whether a resource is managed by Config Sync.
func ManagementEnabled(obj client.Object) bool {
	return core.GetAnnotation(obj, metadata.ResourceManagementKey) == metadata.ResourceManagementEnabled
}

// ManagementDisabled returns true if the resource in the repo explicitly has management disabled.
func ManagementDisabled(obj client.Object) bool {
	return core.GetAnnotation(obj, metadata.ResourceManagementKey) == metadata.ResourceManagementDisabled
}

// ManagedByConfigSync returns true if a resource is managed by Config Sync.
//
// A resource is managed by Config Sync if it meets the following two criteria:
// 1) the `configmanagement.gke.io/managed` anntation is `enabled`;
// 2) the `configsync.gke.io/resource-id` annotation matches the resource.
//
// A resource whose `configmanagement.gke.io/managed` anntation is `enabled` may not be
// managed by Config Sync, because the annotation may be copied from another resource
// managed by Config Sync.
func ManagedByConfigSync(obj client.Object) bool {
	return obj != nil && ManagementEnabled(obj) && core.GetAnnotation(obj, metadata.ResourceIDKey) == core.GKNN(obj)
}

// ManagementUnset returns true if the resource has no Nomos ResourceManagementKey.
func ManagementUnset(obj client.Object) bool {
	as := obj.GetAnnotations()
	if as == nil {
		return true
	}
	_, found := as[metadata.ResourceManagementKey]
	return !found
}
