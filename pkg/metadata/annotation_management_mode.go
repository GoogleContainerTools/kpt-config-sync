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
	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ManagementMode is the type used to identify value enums to use with the
// `configmanagement.gke.io/managed` annotation.
type ManagementMode string

// String returns the string value of the ManagementMode.
// Implements the Stringer interface.
func (m ManagementMode) String() string {
	return string(m)
}

const (
	// ManagementModeAnnotationKey is the annotation that indicates whether
	// Config Sync should manage the content and lifecycle for the object.
	// This annotation is set by Config Sync on a managed resource object.
	ManagementModeAnnotationKey = ConfigManagementPrefix + "managed"
	// ManagementEnabled is the value corresponding to
	// ManagementModeAnnotationKey indicating that Config Sync should manage
	// content and lifecycle for the given resource object.
	ManagementEnabled ManagementMode = "enabled"
	// ManagementDisabled is the value corresponding to
	// ManagementModeAnnotationKey indicating that Config Sync should not manage
	// content and lifecycle for the given resource.
	//
	// By design, the `configmanagement.gke.io/managed: disabled` annotation
	// is set by the user on objects in the source. Config Sync will then remove
	// its metadata from the matching cluster object. The `disabled` value
	// should never be pushed to the cluster.
	ManagementDisabled ManagementMode = "disabled"
)

// WithManagementMode returns a MetaMutator that sets the managed annotation on an Object.
func WithManagementMode(mode ManagementMode) core.MetaMutator {
	return core.Annotation(ManagementModeAnnotationKey, mode.String())
}

// WithoutManagementMode returns a MetaMutator that removes the managed annotation on an Object.
func WithoutManagementMode() core.MetaMutator {
	return core.WithoutAnnotation(ManagementModeAnnotationKey)
}

// IsManagementEnabled returns true if the object has the annotation
// `configmanagement.gke.io/managed: enabled`.
func IsManagementEnabled(obj client.Object) bool {
	return core.GetAnnotation(obj, ManagementModeAnnotationKey) == ManagementEnabled.String()
}

// IsManagementDisabled returns true if the object has the annotation
// `configmanagement.gke.io/managed: disabled`.
func IsManagementDisabled(obj client.Object) bool {
	return core.GetAnnotation(obj, ManagementModeAnnotationKey) == ManagementDisabled.String()
}

// IsManagementUnspecified returns true if the object does NOT have an
// annotation with the key `configmanagement.gke.io/managed`.
func IsManagementUnspecified(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return true
	}
	_, found := annotations[ManagementModeAnnotationKey]
	return !found
}
