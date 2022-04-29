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

// Package util contains shared functionality for constraints and constraint templates.
package util

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
)

// AnnotateErrors sets the error status annotation to the given error messages.
func AnnotateErrors(obj *unstructured.Unstructured, msgs ...string) {
	core.SetAnnotation(obj, metadata.ResourceStatusErrorsKey, jsonify(msgs))
}

// AnnotateReconciling sets the reconciling status annotation to the given reasons.
func AnnotateReconciling(obj *unstructured.Unstructured, msgs ...string) {
	core.SetAnnotation(obj, metadata.ResourceStatusReconcilingKey, jsonify(msgs))
}

// ResetAnnotations removes all status annotations.
func ResetAnnotations(obj *unstructured.Unstructured) {
	core.RemoveAnnotations(obj, metadata.ResourceStatusReconcilingKey)
	core.RemoveAnnotations(obj, metadata.ResourceStatusErrorsKey)
}

// AnnotationsChanged returns true if the status annotations between the two resources.
func AnnotationsChanged(newObj, oldObj *unstructured.Unstructured) bool {
	newAnns := newObj.GetAnnotations()
	oldAnns := oldObj.GetAnnotations()
	return newAnns[metadata.ResourceStatusReconcilingKey] != oldAnns[metadata.ResourceStatusReconcilingKey] ||
		newAnns[metadata.ResourceStatusErrorsKey] != oldAnns[metadata.ResourceStatusErrorsKey]
}
