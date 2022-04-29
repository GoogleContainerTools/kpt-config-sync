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

package queue

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type deleted struct {
	client.Object
}

// DeepCopyObject implements client.Object.
//
// This ensures when we deepcopy a deleted object, we retain that it is deleted.
func (d *deleted) DeepCopyObject() runtime.Object {
	return &deleted{Object: d.Object.DeepCopyObject().(client.Object)}
}

// MarkDeleted marks the given Object as having been deleted from the cluster.
// On receiving a Deleted event, Watchers should call this *first* and then pass
// the returned Object to Add().
func MarkDeleted(ctx context.Context, obj client.Object) client.Object {
	if obj == nil {
		klog.Warning("Attempting to mark nil object as deleted")
		metrics.RecordInternalError(ctx, "remediator")
		return obj
	}
	return &deleted{obj}
}

// WasDeleted returns true if the given Object was marked as having been
// deleted from the cluster.
func WasDeleted(ctx context.Context, obj client.Object) bool {
	if obj == nil {
		klog.Warning("Attempting to check nil object for WasDeleted")
		metrics.RecordInternalError(ctx, "remediator")
		return false
	}
	_, wasDeleted := obj.(*deleted)
	return wasDeleted
}
