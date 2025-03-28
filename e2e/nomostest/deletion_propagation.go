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

package nomostest

import (
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// EnableDeletionPropagation enables foreground deletion propagation on a
// RootSync or RepoSync. The object is annotated locally, but not applied.
// Returns true if a change was made, false if already enabled.
func EnableDeletionPropagation(rs client.Object) bool {
	return SetDeletionPropagationPolicy(rs, metadata.DeletionPropagationPolicyForeground)
}

// DisableDeletionPropagation disables foreground deletion propagation on a
// RootSync or RepoSync. The object annotated is removed locally, but not applied.
// Returns true if a change was made, false if already enabled.
func DisableDeletionPropagation(rs client.Object) bool {
	return RemoveDeletionPropagationPolicy(rs)
}

// IsDeletionPropagationEnabled returns true if deletion propagation annotation
// is set to Foreground.
func IsDeletionPropagationEnabled(rs client.Object) bool {
	return HasDeletionPropagationPolicy(rs, metadata.DeletionPropagationPolicyForeground)
}

// HasDeletionPropagationPolicy returns true if deletion propagation annotation
// is set to the specified policy. Returns false if not set.
func HasDeletionPropagationPolicy(obj client.Object, policy metadata.DeletionPropagationPolicy) bool {
	annotations := obj.GetAnnotations()
	// don't panic if nil
	if len(annotations) == 0 {
		return false
	}
	foundPolicy, found := annotations[metadata.DeletionPropagationPolicyAnnotationKey]
	return found && foundPolicy == string(policy)
}

// SetDeletionPropagationPolicy sets the value of the deletion propagation
// annotation locally (does not apply). Returns true if the object was modified.
func SetDeletionPropagationPolicy(obj client.Object, policy metadata.DeletionPropagationPolicy) bool {
	return core.SetAnnotation(obj, metadata.DeletionPropagationPolicyAnnotationKey, string(policy))
}

// RemoveDeletionPropagationPolicy removes the deletion propagation annotation
// locally (does not apply). Returns true if the object was modified.
func RemoveDeletionPropagationPolicy(obj client.Object) bool {
	return core.RemoveAnnotations(obj, metadata.DeletionPropagationPolicyAnnotationKey)
}
