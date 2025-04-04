// Copyright 2024 Google LLC
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

package finalizer

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type baseFinalizer struct {
	Destroyer applier.Destroyer

	// Client used to update RSync spec and status.
	Client client.Client
}

// destroy calls Destroyer.Destroy, collects errors, and handles logging,
// similar to Updater.apply.
func (bf *baseFinalizer) destroy(ctx context.Context) status.MultiError {
	var err status.MultiError
	eventHandler := func(event applier.Event) {
		if errEvent, ok := event.(applier.ErrorEvent); ok {
			if err == nil {
				err = errEvent.Error
			} else {
				err = status.Append(err, errEvent.Error)
			}
		}
	}
	klog.Info("Destroyer starting...")
	// start := time.Now()
	objStatusMap, syncStats := bf.Destroyer.Destroy(ctx, eventHandler)
	if !syncStats.Empty() {
		klog.Infof("Destroyer made new progress: %s", syncStats.String())
		objStatusMap.Log(klog.V(0))
	}
	// TODO: should we have a destroy duration metric?
	// We don't have the commit here, so we can't send the apply metric.
	// metrics.RecordApplyDuration(ctx, metrics.StatusTagKey(errs), commit, start)
	if err != nil {
		klog.Warningf("Destroyer failed: %v", err)
		return err
	}
	klog.Info("Destroyer succeeded")
	return nil
}

func (bf *baseFinalizer) unmanageObjects(ctx context.Context, rsyncRef client.ObjectKey) status.MultiError {
	var errs status.MultiError
	rg := &v1alpha1.ResourceGroup{}
	if err := bf.Client.Get(ctx, rsyncRef, rg); err != nil {
		return status.APIServerError(err, "failed to get ResourceGroup", rg)
	}
	removeAnnotations := []string{
		metadata.ResourceManagerKey,
		metadata.OwningInventoryKey,
		metadata.ResourceManagementKey,
	}
	for _, objRef := range rg.Spec.Resources {
		// Remove metadata for each object
		obj := &metav1.PartialObjectMetadata{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group: objRef.Group,
			Kind:  objRef.Kind,
		})
		key := client.ObjectKey{
			Name:      objRef.Name,
			Namespace: objRef.Namespace,
		}
		if err := bf.Client.Get(ctx, key, obj); err != nil {
			if apierrors.IsNotFound(err) { // skip orphaning objects that don't exist
				continue
			}
			errs = status.Append(errs, status.APIServerError(err, "failed to get object", obj))
			continue
		}
		existing := obj.DeepCopy()
		if core.RemoveAnnotations(obj, removeAnnotations...) {
			if err := bf.Client.Patch(ctx, obj, client.MergeFrom(existing),
				client.FieldOwner(configsync.FieldManager)); err != nil {
				errs = status.Append(errs, status.APIServerError(err, "failed to patch object to remove metadata", obj))
			}
		}
	}
	return errs
}
