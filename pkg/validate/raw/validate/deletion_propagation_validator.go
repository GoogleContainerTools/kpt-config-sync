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

package validate

import (
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeletionPropagationAnnotation returns an Error if the user-specified
// deletion propagation annotation is invalid.
func DeletionPropagationAnnotation(syncObj client.Object, syncKind string) status.Error {
	aMap := syncObj.GetAnnotations()
	if aMap == nil {
		return nil
	}
	value, found := aMap[metadata.DeletionPropagationPolicyAnnotationKey]
	if !found {
		// missing
		return nil
	}
	switch value {
	case metadata.DeletionPropagationPolicyForeground.String(),
		metadata.DeletionPropagationPolicyOrphan.String():
		// valid
		return nil
	default:
		// invalid
		return NewDeletionPropagationAnnotationError(syncKind)
	}
}

// NewDeletionPropagationAnnotationError returns an error for an invalid
// deletion propagation annotation.
func NewDeletionPropagationAnnotationError(syncKind string) status.Error {
	return invalidSyncBuilder.
		Sprintf("%ss which specify the annotation %q must use one of the values %q or %q",
			syncKind, metadata.DeletionPropagationPolicyAnnotationKey,
			metadata.DeletionPropagationPolicyForeground,
			metadata.DeletionPropagationPolicyOrphan).
		Build()
}
