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

package reconcile

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/differ"
)

// HandleDiff updates objects on the cluster based on the difference between actual and declared resources.
func HandleDiff(ctx context.Context, applier Applier, diff *differ.Diff, recorder record.EventRecorder) (bool, status.Error) {
	switch diff.Type() {
	case differ.NoOp:
		return false, nil
	case differ.Create:
		enableManagement(diff.Declared)
		return applier.Create(ctx, diff.Declared)
	case differ.Update:
		enableManagement(diff.Declared)
		return applier.Update(ctx, diff.Declared, diff.Actual)
	case differ.Delete:
		return applier.Delete(ctx, diff.Actual)
	case differ.Unmanage:
		// The intended state of an unmanaged resource is a copy of the resource, but without management enabled.
		// See b/157751323 for context on why we are doing a specific Remove() here instead of a generic Update().
		return applier.RemoveNomosMeta(ctx, diff.Actual)
	case differ.Error:
		warnInvalidAnnotationResource(recorder, diff.Declared)
		return false, nil
	}

	return false, status.InternalErrorf("programmatic error, unhandled syncer diff type: %v", diff.Type())
}

func warnInvalidAnnotationResource(recorder record.EventRecorder, declared *unstructured.Unstructured) {
	err := nonhierarchical.IllegalManagementAnnotationError(
		declared,
		declared.GetAnnotations()[metadata.ResourceManagementKey],
	)
	klog.Warning(err)
	recorder.Event(declared, corev1.EventTypeWarning, v1.EventReasonInvalidAnnotation, err.Error())
}
