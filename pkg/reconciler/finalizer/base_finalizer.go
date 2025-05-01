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
	"kpt.dev/configsync/pkg/applier/stats"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type baseFinalizer struct {
	Destroyer applier.Destroyer

	// Client used to update RSync spec and status.
	Client client.Client

	// ApplySetID used to remove the apply-set label when orphaning objects
	ApplySetID string
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

func (bf *baseFinalizer) unmanageObjects(ctx context.Context, rsyncRef client.ObjectKey, hasSynced bool) status.MultiError {
	var errs status.MultiError
	rg := &v1alpha1.ResourceGroup{}
	if err := bf.Client.Get(ctx, rsyncRef, rg); err != nil {
		if apierrors.IsNotFound(err) {
			if hasSynced {
				// The ResourceGroup should exist if there has been a sync
				klog.Error("ResourceGroup not found when attempting to unmanage objects for synced RSync")
			} else {
				// ResourceGroup doesn't exist, but that's expected since there hasn't been a sync
				klog.Info("ResourceGroup not found when attempting to unmanage objects for unsynced RSync")
			}
			return nil
		}
		return status.APIServerError(err, "failed to get ResourceGroup", rg)
	}
	objStatusMap := make(applier.ObjectStatusMap)
	syncStats := stats.NewSyncStats()
	for _, objRef := range rg.Spec.Resources {
		// Remove metadata for each object
		actuationStatus, err := bf.unmanageObject(ctx, objRef)
		objStatusMap[coreIDFromObjMetadata(objRef)] = &applier.ObjectStatus{
			Strategy:  actuation.ActuationStrategyApply,
			Actuation: actuationStatus,
		}
		syncStats.ApplyEvent.Add(actuationStatusToApplyEvent(actuationStatus))
		errs = status.Append(errs, err)
	}

	klog.Infof("Unmanager made new progress: %s", syncStats.String())
	objStatusMap.Log(klog.V(0))
	return errs
}

func (bf *baseFinalizer) unmanageObject(ctx context.Context, objRef v1alpha1.ObjMetadata) (actuation.ActuationStatus, error) {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{ // Version not required for PartialObjectMetadata
		Group: objRef.Group,
		Kind:  objRef.Kind,
	})
	key := client.ObjectKey{
		Name:      objRef.Name,
		Namespace: objRef.Namespace,
	}
	id := coreIDFromObjMetadata(objRef)
	if err := bf.Client.Get(ctx, key, obj); err != nil {
		if apierrors.IsNotFound(err) { // skip orphaning objects that don't exist
			klog.Infof("Skip unmanaging %s (Not Found)", id.String())
			return actuation.ActuationSkipped, nil
		}
		return actuation.ActuationFailed, status.APIServerError(err, "failed to get object", obj)
	}
	existing := obj.DeepCopy()
	// TODO: always remove apply set label with RemoveConfigSyncMetadata?
	// All callers of RemoveConfigSyncMetadata also call RemoveApplySetPartOfLabel
	update1 := metadata.RemoveConfigSyncMetadata(obj)
	update2 := metadata.RemoveApplySetPartOfLabel(obj, bf.ApplySetID)
	if update1 || update2 {
		if err := bf.Client.Patch(ctx, obj, client.MergeFrom(existing),
			client.FieldOwner(configsync.FieldManager)); err != nil {
			return actuation.ActuationFailed, status.APIServerError(err, "failed to patch object to remove metadata", obj)
		}
		return actuation.ActuationSucceeded, nil
	}
	klog.Infof("Skip unmanaging %s (no-op)", id.String())
	return actuation.ActuationSkipped, nil
}

func coreIDFromObjMetadata(objRef v1alpha1.ObjMetadata) core.ID {
	return core.ID{
		GroupKind: schema.GroupKind{
			Group: objRef.Group,
			Kind:  objRef.Group,
		},
		ObjectKey: client.ObjectKey{
			Name:      objRef.Name,
			Namespace: objRef.Namespace,
		},
	}
}

// actuationStatusToApplyEvent maps ActuationStatus to ApplyEventStatus as a helper
// method for the unmanager. This emulates the behavior of the applier so that
// the unmanager can print logs of the same structure.
func actuationStatusToApplyEvent(s actuation.ActuationStatus) event.ApplyEventStatus {
	switch s {
	case actuation.ActuationSucceeded:
		return event.ApplySuccessful
	case actuation.ActuationSkipped:
		return event.ApplySkipped
	case actuation.ActuationPending:
		return event.ApplyPending
	default:
		return event.ApplyFailed
	}
}
