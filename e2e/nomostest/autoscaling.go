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

// EnableReconcilerAutoscaling enables reconciler autoscaling on a
// RootSync or RepoSync. The object is annotated locally, but not applied.
// Returns true if a change was made, false if already enabled.
func EnableReconcilerAutoscaling(rs client.Object) bool {
	return SetReconcilerAutoscalingStrategy(rs, metadata.ReconcilerAutoscalingStrategyAuto)
}

// DisableReconcilerAutoscaling disables reconciler autoscaling on a
// RootSync or RepoSync. The object annotated is removed locally, but not applied.
// Returns true if a change was made, false if already enabled.
func DisableReconcilerAutoscaling(rs client.Object) bool {
	return RemoveReconcilerAutoscalingStrategy(rs)
}

// IsReconcilerAutoscalingEnabled returns true if reconciler-autoscaling-strategy
// annotation is set to Foreground.
func IsReconcilerAutoscalingEnabled(rs client.Object) bool {
	return HasReconcilerAutoscalingStrategy(rs, metadata.ReconcilerAutoscalingStrategyAuto)
}

// HasReconcilerAutoscalingStrategy returns true if reconciler-autoscaling-strategy
// annotation is set to the specified policy. Returns false if not set.
func HasReconcilerAutoscalingStrategy(obj client.Object, policy metadata.ReconcilerAutoscalingStrategy) bool {
	annotations := obj.GetAnnotations()
	// don't panic if nil
	if len(annotations) == 0 {
		return false
	}
	foundPolicy, found := annotations[metadata.ReconcilerAutoscalingStrategyAnnotationKey]
	return found && foundPolicy == string(policy)
}

// SetReconcilerAutoscalingStrategy sets the value of the reconciler-autoscaling-strategy
// annotation locally (does not apply). Returns true if the object was modified.
func SetReconcilerAutoscalingStrategy(obj client.Object, policy metadata.ReconcilerAutoscalingStrategy) bool {
	if HasReconcilerAutoscalingStrategy(obj, policy) {
		return false
	}
	core.SetAnnotation(obj, metadata.ReconcilerAutoscalingStrategyAnnotationKey, string(policy))
	return true
}

// RemoveReconcilerAutoscalingStrategy removes the reconciler-autoscaling-strategy
// annotation locally (does not apply). Returns true if the object was modified.
func RemoveReconcilerAutoscalingStrategy(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	// don't panic if nil
	if len(annotations) == 0 {
		return false
	}
	if _, found := annotations[metadata.ReconcilerAutoscalingStrategyAnnotationKey]; !found {
		return false
	}
	delete(annotations, metadata.ReconcilerAutoscalingStrategyAnnotationKey)
	obj.SetAnnotations(annotations)
	return true
}
