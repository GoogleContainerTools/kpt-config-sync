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

package namespacecontroller

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/syncer/differ"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var _ predicate.Predicate = UnmanagedNamespacePredicate{}

// UnmanagedNamespacePredicate triggers a new reconciliation of the namespace
// controller when receiving an unmanaged Namespace event.
type UnmanagedNamespacePredicate struct {
	predicate.Funcs
}

// isUnmanagedNamespace returns whether the Namespace is not managed by Config Sync.
// If it is a managed Namespace, the event should be reconciled by the remediator.
func isUnmanagedNamespace(obj client.Object) bool {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		// Skip the event if it is not a Namespace object.
		return false
	}
	// Skip the event if the Namespace is already managed by CS
	return !differ.ManagedByConfigSync(ns)
}

// Create implements default CreateEvent filter for validating namespace with additional labels.
func (n UnmanagedNamespacePredicate) Create(e event.CreateEvent) bool {
	return isUnmanagedNamespace(e.Object)
}

// Delete implements default DeleteEvent filter for validating namespace with additional labels.
func (n UnmanagedNamespacePredicate) Delete(_ event.DeleteEvent) bool {
	// It is not reliable to check if the deleted event is managed by Config Sync
	// based on the last known labels because it may not match the actual last state.
	// So it always return true to enqueue the request.
	return true
}

// Update implements default UpdateEvent filter for validating namespace label change.
func (n UnmanagedNamespacePredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil {
		klog.Errorf("update event has no old object to update, event: %v", e)
		return false
	}
	if e.ObjectNew == nil {
		klog.Errorf("update event has no new object to update, event: %v", e)
		return false
	}

	return isUnmanagedNamespace(e.ObjectOld) || isUnmanagedNamespace(e.ObjectNew)
}

// Generic implements Predicate.
func (n UnmanagedNamespacePredicate) Generic(e event.GenericEvent) bool {
	return isUnmanagedNamespace(e.Object)
}
