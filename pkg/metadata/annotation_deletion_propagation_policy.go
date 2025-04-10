// Copyright 2025 Google LLC
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

package metadata

import (
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeletionPropagationPolicy is the type used to identify value enums to use
// with the deletion-propagation-policy annotation.
type DeletionPropagationPolicy string

// String returns the string value of the DeletionPropagationPolicy.
// Implements the Stringer interface.
func (p DeletionPropagationPolicy) String() string {
	return string(p)
}

const (
	// DeletionPropagationPolicyAnnotationKey is the annotation key set on
	// RootSync/RepoSync objects to indicate what do do with the managed
	// resources when the RootSync/RepoSync object is deleted.
	DeletionPropagationPolicyAnnotationKey = configsync.ConfigSyncPrefix + "deletion-propagation-policy"
	// DeletionPropagationPolicyForeground indicates that the managed resources
	// should all be deleted/pruned before the RootSync/RepoSync object is deleted.
	// This will block deletion of the RootSync/RepoSync using a finalizer.
	DeletionPropagationPolicyForeground DeletionPropagationPolicy = "Foreground"
	// DeletionPropagationPolicyOrphan indicates that the managed resources
	// should all be orphaned (unmanaged but not deleted) when the
	// RootSync/RepoSync object is deleted.
	// This is the default behavior if the annotation is not specified.
	DeletionPropagationPolicyOrphan DeletionPropagationPolicy = "Orphan"
)

// HasDeletionPropagationPolicy returns true if deletion propagation annotation
// is set to the specified policy. Returns false if not set.
func HasDeletionPropagationPolicy(obj client.Object, policy DeletionPropagationPolicy) bool {
	return core.GetAnnotation(obj, DeletionPropagationPolicyAnnotationKey) == policy.String()
}

// SetDeletionPropagationPolicy sets the value of the deletion propagation
// annotation locally (does not apply). Returns true if the object was modified.
func SetDeletionPropagationPolicy(obj client.Object, policy DeletionPropagationPolicy) bool {
	return core.SetAnnotation(obj, DeletionPropagationPolicyAnnotationKey, policy.String())
}

// RemoveDeletionPropagationPolicy removes the deletion propagation annotation
// locally (does not apply). Returns true if the object was modified.
func RemoveDeletionPropagationPolicy(obj client.Object) bool {
	return core.RemoveAnnotations(obj, DeletionPropagationPolicyAnnotationKey)
}

// WithDeletionPropagationPolicy returns a MetaMutator that sets the
// DeletionPropagationPolicy annotation on an Object.
func WithDeletionPropagationPolicy(mode DeletionPropagationPolicy) core.MetaMutator {
	return core.Annotation(DeletionPropagationPolicyAnnotationKey, mode.String())
}

// WithoutDeletionPropagationPolicy returns a MetaMutator that removes the
// DeletionPropagationPolicy annotation on an Object.
func WithoutDeletionPropagationPolicy() core.MetaMutator {
	return core.WithoutAnnotation(DeletionPropagationPolicyAnnotationKey)
}

// IsDeletionPropagationForeground returns true if the object has the annotation
// `configsync.gke.io/deletion-propagation-policy: Foreground`.
func IsDeletionPropagationForeground(obj client.Object) bool {
	return core.GetAnnotation(obj, DeletionPropagationPolicyAnnotationKey) == DeletionPropagationPolicyForeground.String()
}

// IsDeletionPropagationOrphan returns true if the object has the annotation
// `configsync.gke.io/deletion-propagation-policy: Orphan`.
func IsDeletionPropagationOrphan(obj client.Object) bool {
	return core.GetAnnotation(obj, DeletionPropagationPolicyAnnotationKey) == DeletionPropagationPolicyOrphan.String()
}

// IsDeletionPropagationUnspecified returns true if the object does NOT have an
// annotation with the key `configsync.gke.io/deletion-propagation-policy`.
func IsDeletionPropagationUnspecified(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return true
	}
	_, found := annotations[DeletionPropagationPolicyAnnotationKey]
	return !found
}
