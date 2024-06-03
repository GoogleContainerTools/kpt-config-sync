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

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/mutate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// RootSyncFinalizer handles finalizing RootSync objects, using the Destroyer
// to destroy all managed user objects previously applied from source.
// Implements the Finalizer interface.
type RootSyncFinalizer struct {
	baseFinalizer

	// Client used to update RSync spec and status.
	Client client.Client

	// StopControllers will be called by the Finalize() method to stop the Parser and Remediator.
	StopControllers context.CancelFunc

	// ControllersStopped will be closed by the caller when the parser and
	// remediator have fully stopped. This unblocks Finalize() to destroy
	// managed resource objects.
	ControllersStopped <-chan struct{}
}

// Finalize performs the following actions on the syncObj (RootSync):
// - Stop other controllers
// - Wait for other controllers to stop
// - Sets the Finalizing condition
// - Uses the Destroyer to delete managed objects
// - Removes the Finalizing condition
// - Removes the Finalizer (unblocking deletion)
//
// The specified syncObj must be of type `*v1beta1.RootSync`.
func (f *RootSyncFinalizer) Finalize(ctx context.Context, syncObj client.Object) error {
	rs, ok := syncObj.(*v1beta1.RootSync)
	if !ok {
		return fmt.Errorf("invalid syncObj type: expected *v1beta1.RootSync, but got %T", syncObj)
	}

	// Stop the parser & remediator
	klog.Info("Finalizer scheduled: Parser & Remediator stopping")
	f.StopControllers()

	// Wait for parser & remediator to stop
	<-f.ControllersStopped
	klog.Info("Finalizer executing: Parser & Remediator stopped")

	if _, err := f.setFinalizingCondition(ctx, rs); err != nil {
		return fmt.Errorf("setting Finalizing condition: %w", err)
	}

	if err := f.deleteManagedObjects(ctx, rs); err != nil {
		return fmt.Errorf("deleting managed objects: %w", err)
	}
	klog.Infof("Deletion of managed objects successful")

	// TODO: optimize by combining these updates into a single update
	if _, err := f.removeFinalizingCondition(ctx, rs); err != nil {
		return fmt.Errorf("removing Finalizing condition: %w", err)
	}
	if _, err := f.RemoveFinalizer(ctx, rs); err != nil {
		return fmt.Errorf("removing finalizer: %w", err)
	}
	return nil
}

// AddFinalizer adds the `configsync.gke.io/reconciler` finalizer to the
// specified object, and updates the server.
//
// The specified syncObj must be of type `*v1beta1.RootSync`.
func (f *RootSyncFinalizer) AddFinalizer(ctx context.Context, syncObj client.Object) (bool, error) {
	updated, err := mutate.Spec(ctx, f.Client, syncObj, func() error {
		if !addFinalizer(syncObj) {
			// Already added. No change necessary.
			return &mutate.NoUpdateError{}
		}
		return nil
	}, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return updated, fmt.Errorf("failed to add finalizer: %w", err)
	}
	if updated {
		klog.Info("Finalizer injection successful")
	} else {
		klog.V(5).Info("Finalizer injection skipped: already exists")
	}
	return updated, nil
}

// RemoveFinalizer removes the `configsync.gke.io/reconciler` finalizer from the
// specified object, and updates the server.
//
// The specified syncObj must be of type `*v1beta1.RootSync`.
func (f *RootSyncFinalizer) RemoveFinalizer(ctx context.Context, syncObj client.Object) (bool, error) {
	updated, err := mutate.Spec(ctx, f.Client, syncObj, func() error {
		if !removeFinalizer(syncObj) {
			// Already removed. No change necessary.
			return &mutate.NoUpdateError{}
		}
		return nil
	}, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return updated, fmt.Errorf("failed to remove finalizer: %w", err)
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
func (f *RootSyncFinalizer) setFinalizingCondition(ctx context.Context, syncObj *v1beta1.RootSync) (bool, error) {
	updated, err := mutate.Status(ctx, f.Client, syncObj, func() error {
		if !rootsync.SetReconcilerFinalizing(syncObj, "ResourcesDeleting", "Deleting managed resource objects") {
			// Already removed. No change necessary.
			return &mutate.NoUpdateError{}
		}
		return nil
	}, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return updated, fmt.Errorf("failed to set ReconcilerFinalizing condition: %w", err)
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
func (f *RootSyncFinalizer) removeFinalizingCondition(ctx context.Context, syncObj *v1beta1.RootSync) (bool, error) {
	updated, err := mutate.Status(ctx, f.Client, syncObj, func() error {
		if !rootsync.RemoveCondition(syncObj, v1beta1.RootSyncReconcilerFinalizing) {
			// Already removed. No change necessary.
			return &mutate.NoUpdateError{}
		}
		return nil
	}, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return updated, fmt.Errorf("failed to remove ReconcilerFinalizing condition: %w", err)
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
func (f *RootSyncFinalizer) deleteManagedObjects(ctx context.Context, syncObj *v1beta1.RootSync) error {
	destroyErrs := f.destroy(ctx)
	// Update the FinalizerFailure condition whether the destroy succeeded or failed
	if _, updateErr := f.updateFailureCondition(ctx, syncObj, destroyErrs); updateErr != nil {
		updateErr = fmt.Errorf("updating FinalizerFailure condition: %w", updateErr)
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
func (f *RootSyncFinalizer) updateFailureCondition(ctx context.Context, syncObj *v1beta1.RootSync, destroyErrs status.MultiError) (bool, error) {
	updated, err := mutate.Status(ctx, f.Client, syncObj, func() error {
		if destroyErrs != nil {
			if !rootsync.SetReconcilerFinalizerFailure(syncObj, destroyErrs) {
				// Already removed. No change necessary.
				return &mutate.NoUpdateError{}
			}
		} else {
			if !rootsync.RemoveCondition(syncObj, v1beta1.RootSyncReconcilerFinalizerFailure) {
				// Already updated. No change necessary.
				return &mutate.NoUpdateError{}
			}
		}
		return nil
	}, client.FieldOwner(configsync.FieldManager))
	if err != nil {
		return updated, fmt.Errorf("failed to set ReconcilerFinalizerFailure condition: %w", err)
	}
	if updated {
		klog.Info("ReconcilerFinalizerFailure condition update successful")
	} else {
		klog.V(5).Info("ReconcilerFinalizerFailure condition update skipped: already set")
	}
	return updated, nil
}
