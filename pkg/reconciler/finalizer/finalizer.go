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

package finalizer

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// Finalizer handles finalizing a RootSync or RepoSync for the reconciler.
type Finalizer struct {
	Destroyer applier.Destroyer
	Client    client.Client

	// StopControllers will be called by the Finalize() method to stop the Parser and Remediator.
	StopControllers context.CancelFunc

	// ControllersStopped will be closed by the caller when the parser and
	// remediator have fully stopped. This unblocks Finalize() to destroy
	// managed resource objects.
	ControllersStopped <-chan struct{}
}

// Finalize performs the following actions on the RootSync or RepoSync:
// - Stop other controllers
// - Wait for other controllers to stop
// - Sets the Finalizing condition
// - Uses the Destroyer to delete managed objects
// - Removes the Finalizing condition
// - Removes the Finalizer (unblocking deletion)
func (f *Finalizer) Finalize(ctx context.Context, obj *unstructured.Unstructured) error {
	// Stop the parser & remediator
	klog.Info("Finalizer scheduled: Parser & Remediator stopping")
	f.StopControllers()

	// Wait for parser & remediator to stop
	<-f.ControllersStopped
	klog.Info("Finalizer executing: Parser & Remediator stopped")

	if _, err := f.setFinalizingCondition(ctx, obj); err != nil {
		return errors.Wrap(err, "setting Finalizing condition")
	}

	if err := f.deleteManagedObjects(ctx, obj); err != nil {
		return errors.Wrap(err, "deleting managed objects")
	}
	klog.Infof("Deletion of managed objects successful")

	// TODO: optimize by combining these updates into a single update
	if _, err := f.removeFinalizingCondition(ctx, obj); err != nil {
		return errors.Wrap(err, "removing Finalizing condition")
	}
	if _, err := f.RemoveFinalizer(ctx, obj); err != nil {
		return errors.Wrap(err, "removing finalizer")
	}
	return nil
}

// AddFinalizer adds the `configsync.gke.io/reconciler` finalizer to the specified
// object.
func (f *Finalizer) AddFinalizer(ctx context.Context, obj *unstructured.Unstructured) (bool, error) {
	updated, err := updateObjectWithRetry(ctx, f.Client, obj, func() error {
		if !controllerutil.AddFinalizer(obj, metadata.ReconcilerFinalizer) {
			// Already added. No change necessary.
			return &NoUpdateError{}
		}
		return nil
	})
	if err != nil {
		return updated, errors.Wrapf(err, "failed to remove finalizer")
	}
	if updated {
		klog.Info("Finalizer injection successful")
	} else {
		klog.V(5).Info("Finalizer injection skipped: already exists")
	}
	return updated, nil
}

// RemoveFinalizer removes the `configsync.gke.io/reconciler` finalizer from the
// specified object.
func (f *Finalizer) RemoveFinalizer(ctx context.Context, obj *unstructured.Unstructured) (bool, error) {
	updated, err := updateObjectWithRetry(ctx, f.Client, obj, func() error {
		if !controllerutil.RemoveFinalizer(obj, metadata.ReconcilerFinalizer) {
			// Already removed. No change necessary.
			return &NoUpdateError{}
		}
		return nil
	})
	if err != nil {
		return updated, errors.Wrapf(err, "failed to remove finalizer")
	}
	if updated {
		klog.Info("Finalizer removal successful")
	} else {
		klog.V(5).Info("Finalizer removal skipped: already removed")
	}
	return updated, nil
}

// setFinalizingCondition sets the ReconcilerFinalizing condition on the
// specified object.
func (f *Finalizer) setFinalizingCondition(ctx context.Context, obj *unstructured.Unstructured) (bool, error) {
	updated, err := updateObjectWithRetry(ctx, f.Client, obj, func() error {
		// Convert to RootSync/RepoSync to make it easier to update the conditions.
		// TODO: Optimize by avoiding multiple type conversions.
		rObj, err := kinds.ToTypedObject(obj, core.Scheme)
		if err != nil {
			return err
		}
		switch tObj := rObj.(type) {
		case *v1beta1.RootSync:
			if !rootsync.SetReconcilerFinalizing(tObj, "ResourcesDeleting", "Deleting managed resource objects") {
				// Already removed. No change necessary.
				return &NoUpdateError{}
			}
		case *v1beta1.RepoSync:
			if !reposync.SetReconcilerFinalizing(tObj, "ResourcesDeleting", "Deleting managed resource objects") {
				// Already removed. No change necessary.
				return &NoUpdateError{}
			}
		case client.Object:
			return fmt.Errorf("invalid object type: expected v1beta1.RootSync or v1beta1.RepoSync: %s", objSummary(tObj))
		default:
			return fmt.Errorf("invalid object type: expected v1beta1.RootSync or v1beta1.RepoSync: %T", rObj)
		}
		// Copy updates back into the original unstructured object
		uObj, err := kinds.ToUnstructured(rObj, core.Scheme)
		if err != nil {
			return err
		}
		uObj.DeepCopyInto(obj)
		return nil
	})
	if err != nil {
		return updated, errors.Wrapf(err, "failed to set ReconcilerFinalizing condition")
	}
	if updated {
		klog.Info("ReconcilerFinalizing condition update successful")
	} else {
		klog.V(5).Info("ReconcilerFinalizing condition update skipped: already set")
	}
	return updated, nil
}

// removeFinalizingCondition removes the ReconcilerFinalizing condition from the
// specified object.
func (f *Finalizer) removeFinalizingCondition(ctx context.Context, obj *unstructured.Unstructured) (bool, error) {
	updated, err := updateObjectWithRetry(ctx, f.Client, obj, func() error {
		// Convert to RootSync/RepoSync to make it easier to update the conditions.
		// TODO: Optimize by avoiding multiple type conversions.
		rObj, err := kinds.ToTypedObject(obj, core.Scheme)
		if err != nil {
			return err
		}
		switch tObj := rObj.(type) {
		case *v1beta1.RootSync:
			if !rootsync.RemoveCondition(tObj, v1beta1.RootSyncReconcilerFinalizing) {
				// Already removed. No change necessary.
				return &NoUpdateError{}
			}
		case *v1beta1.RepoSync:
			if !reposync.RemoveCondition(tObj, v1beta1.RepoSyncReconcilerFinalizing) {
				// Already removed. No change necessary.
				return &NoUpdateError{}
			}
		case client.Object:
			return fmt.Errorf("invalid object type: expected v1beta1.RootSync or v1beta1.RepoSync: %s", objSummary(tObj))
		default:
			return fmt.Errorf("invalid object type: expected v1beta1.RootSync or v1beta1.RepoSync: %T", rObj)
		}
		// Copy updates back into the original unstructured object
		uObj, err := kinds.ToUnstructured(rObj, core.Scheme)
		if err != nil {
			return err
		}
		uObj.DeepCopyInto(obj)
		return nil
	})
	if err != nil {
		return updated, errors.Wrapf(err, "failed to remove ReconcilerFinalizing condition")
	}
	if updated {
		klog.Info("ReconcilerFinalizing condition removal successful")
	} else {
		klog.V(5).Info("ReconcilerFinalizing condition removal skipped: already removed")
	}
	return updated, nil
}

// deleteManagedObjects uses the destroyer to delete managed objects and then
// updates the ReconcilerFinalizerFailure condition on the specified object.
func (f *Finalizer) deleteManagedObjects(ctx context.Context, syncObj *unstructured.Unstructured) error {
	destroyErrs := f.Destroyer.Destroy(ctx)
	// Update the FinalizerFailure condition whether the destroy succeeded or failed
	if _, updateErr := f.updateFailureCondition(ctx, syncObj, destroyErrs); updateErr != nil {
		updateErr = errors.Wrap(updateErr, "updating FinalizerFailure condition")
		if destroyErrs != nil {
			return status.Append(destroyErrs, updateErr)
		}
		return updateErr
	}
	if destroyErrs != nil {
		return destroyErrs
	}
	return nil
}

// updateFailureCondition sets or removes the ReconcilerFinalizerFailure
// condition on the specified object, depending if errors were specified or not.
func (f *Finalizer) updateFailureCondition(ctx context.Context, obj *unstructured.Unstructured, destroyErrs status.MultiError) (bool, error) {
	updated, err := updateObjectStatus(ctx, f.Client, obj, func() error {
		// Convert to RootSync/RepoSync to make it easier to update the conditions.
		// TODO: Optimize by avoiding multiple type conversions.
		rObj, err := kinds.ToTypedObject(obj, core.Scheme)
		if err != nil {
			return err
		}
		switch tObj := rObj.(type) {
		case *v1beta1.RootSync:
			if destroyErrs != nil {
				if !rootsync.SetReconcilerFinalizerFailure(tObj, destroyErrs) {
					// Already removed. No change necessary.
					return &NoUpdateError{}
				}
			} else {
				if !rootsync.RemoveCondition(tObj, v1beta1.RootSyncReconcilerFinalizerFailure) {
					// Already updated. No change necessary.
					return &NoUpdateError{}
				}
			}
		case *v1beta1.RepoSync:
			if destroyErrs != nil {
				if !reposync.SetReconcilerFinalizerFailure(tObj, destroyErrs) {
					// Already updated. No change necessary.
					return &NoUpdateError{}
				}
			} else {
				if !reposync.RemoveCondition(tObj, v1beta1.RepoSyncReconcilerFinalizerFailure) {
					// Already removed. No change necessary.
					return &NoUpdateError{}
				}
			}
		case client.Object:
			return fmt.Errorf("invalid object type: expected v1beta1.RootSync or v1beta1.RepoSync: %s", objSummary(tObj))
		default:
			return fmt.Errorf("invalid object type: expected v1beta1.RootSync or v1beta1.RepoSync: %T", rObj)
		}
		// Copy updates back into the original unstructured object
		uObj, err := kinds.ToUnstructured(rObj, core.Scheme)
		if err != nil {
			return err
		}
		uObj.DeepCopyInto(obj)
		return nil
	})
	if err != nil {
		return updated, errors.Wrapf(err, "failed to set ReconcilerFinalizerFailure condition")
	}
	if updated {
		klog.Info("ReconcilerFinalizerFailure condition update successful")
	} else {
		klog.V(5).Info("ReconcilerFinalizerFailure condition update skipped: already set")
	}
	return updated, nil
}

func objSummary(obj client.Object) string {
	return fmt.Sprintf("%T %s %s/%s",
		obj, obj.GetObjectKind().GroupVersionKind(),
		obj.GetNamespace(), obj.GetName())
}
