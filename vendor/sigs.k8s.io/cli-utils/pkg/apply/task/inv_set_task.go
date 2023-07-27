// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package task

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/taskrunner"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// DeleteOrUpdateInvTask encapsulates structures necessary to set the
// inventory references at the end of the apply/prune.
type DeleteOrUpdateInvTask struct {
	TaskName      string
	InvClient     inventory.Client
	InvInfo       inventory.Info
	PrevInventory object.ObjMetadataSet
	DryRun        common.DryRunStrategy
	// if Destroy is set, the inventory will be deleted if all objects were successfully pruned
	Destroy bool
}

func (i *DeleteOrUpdateInvTask) Name() string {
	return i.TaskName
}

func (i *DeleteOrUpdateInvTask) Action() event.ResourceAction {
	return event.InventoryAction
}

func (i *DeleteOrUpdateInvTask) Identifiers() object.ObjMetadataSet {
	return object.ObjMetadataSet{}
}

// Start will reconcile the state of the inventory, depending on the value of
// Destroy.
//
// If Destroy is set, the intent is to delete the inventory. The inventory will
// only be deleted if all prunes were successful (none failed/skipped). If any
// prunes were failed or skipped, the inventory will be updated.
//
// If Destroy is false, the inventory will be updated.
func (i *DeleteOrUpdateInvTask) Start(taskContext *taskrunner.TaskContext) {
	go func() {
		var err error
		if i.Destroy && i.destroySuccessful(taskContext) {
			err = i.deleteInventory()
		} else {
			err = i.updateInventory(taskContext)
		}
		taskContext.TaskChannel() <- taskrunner.TaskResult{Err: err}
	}()
}

// Cancel is not supported by the DeleteOrUpdateInvTask.
func (i *DeleteOrUpdateInvTask) Cancel(_ *taskrunner.TaskContext) {}

// StatusUpdate is not supported by the DeleteOrUpdateInvTask.
func (i *DeleteOrUpdateInvTask) StatusUpdate(_ *taskrunner.TaskContext, _ object.ObjMetadata) {}

// updateInventory sets (creates or replaces) the inventory.
//
// The guiding principal is that anything in the cluster should be in the
// inventory, unless it was explicitly abandoned.
//
// This task must run after all the apply and prune tasks have completed.
//
// Added objects:
// - Applied resources (successful)
//
// Retained objects:
// - Applied resources (filtered/skipped)
// - Applied resources (failed)
// - Deleted resources (filtered/skipped) that were not abandoned
// - Deleted resources (failed)
// - Abandoned resources (failed)
//
// Removed objects:
// - Deleted resources (successful)
// - Abandoned resources (successful)
func (i *DeleteOrUpdateInvTask) updateInventory(taskContext *taskrunner.TaskContext) error {
	klog.V(2).Infof("inventory set task starting (name: %q)", i.TaskName)
	invObjs := object.ObjMetadataSet{}

	// TODO: Just use InventoryManager.Store()
	im := taskContext.InventoryManager()

	// If an object applied successfully, keep or add it to the inventory.
	appliedObjs := im.SuccessfulApplies()
	klog.V(4).Infof("set inventory %d successful applies", len(appliedObjs))
	invObjs = invObjs.Union(appliedObjs)

	// If an object failed to apply and was previously stored in the inventory,
	// then keep it in the inventory so it can be applied/pruned next time.
	// This will remove new resources that failed to apply from the inventory,
	// because even tho they were added by InvAddTask, the PrevInventory
	// represents the inventory before the pipeline has run.
	applyFailures := i.PrevInventory.Intersection(im.FailedApplies())
	klog.V(4).Infof("keep in inventory %d failed applies", len(applyFailures))
	invObjs = invObjs.Union(applyFailures)

	// If an object skipped apply and was previously stored in the inventory,
	// then keep it in the inventory so it can be applied/pruned next time.
	// It's likely that all the skipped applies are already in the inventory,
	// because the apply filters all currently depend on cluster state,
	// but we're doing the intersection anyway just to be sure.
	applySkips := i.PrevInventory.Intersection(im.SkippedApplies())
	klog.V(4).Infof("keep in inventory %d skipped applies", len(applySkips))
	invObjs = invObjs.Union(applySkips)

	// If an object failed to delete and was previously stored in the inventory,
	// then keep it in the inventory so it can be applied/pruned next time.
	// It's likely that all the delete failures are already in the inventory,
	// because the set of resources to prune comes from the inventory,
	// but we're doing the intersection anyway just to be sure.
	pruneFailures := i.PrevInventory.Intersection(im.FailedDeletes())
	klog.V(4).Infof("set inventory %d failed prunes", len(pruneFailures))
	invObjs = invObjs.Union(pruneFailures)

	// If an object skipped delete and was previously stored in the inventory,
	// then keep it in the inventory so it can be applied/pruned next time.
	// It's likely that all the skipped deletes are already in the inventory,
	// because the set of resources to prune comes from the inventory,
	// but we're doing the intersection anyway just to be sure.
	pruneSkips := i.PrevInventory.Intersection(im.SkippedDeletes())
	klog.V(4).Infof("keep in inventory %d skipped prunes", len(pruneSkips))
	invObjs = invObjs.Union(pruneSkips)

	// If an object failed to reconcile and was previously stored in the inventory,
	// then keep it in the inventory so it can be waited on next time.
	reconcileFailures := i.PrevInventory.Intersection(im.FailedReconciles())
	klog.V(4).Infof("set inventory %d failed reconciles", len(reconcileFailures))
	invObjs = invObjs.Union(reconcileFailures)

	// If an object timed out reconciling and was previously stored in the inventory,
	// then keep it in the inventory so it can be waited on next time.
	reconcileTimeouts := i.PrevInventory.Intersection(im.TimeoutReconciles())
	klog.V(4).Infof("keep in inventory %d timeout reconciles", len(reconcileTimeouts))
	invObjs = invObjs.Union(reconcileTimeouts)

	// If an object is abandoned, then remove it from the inventory.
	abandonedObjects := taskContext.AbandonedObjects()
	klog.V(4).Infof("remove from inventory %d abandoned objects", len(abandonedObjects))
	invObjs = invObjs.Diff(abandonedObjects)

	// If an object is invalid and was previously stored in the inventory,
	// then keep it in the inventory so it can be applied/pruned next time.
	invalidObjects := i.PrevInventory.Intersection(taskContext.InvalidObjects())
	klog.V(4).Infof("keep in inventory %d invalid objects", len(invalidObjects))
	invObjs = invObjs.Union(invalidObjects)

	klog.V(4).Infof("get the apply status for %d objects", len(invObjs))
	objStatus := taskContext.InventoryManager().Inventory().Status.Objects

	klog.V(4).Infof("set inventory %d total objects", len(invObjs))
	err := i.InvClient.Replace(i.InvInfo, invObjs, objStatus, i.DryRun)

	klog.V(2).Infof("inventory set task completing (name: %q)", i.TaskName)
	return err
}

// deleteInventory deletes the inventory object from the cluster.
func (i *DeleteOrUpdateInvTask) deleteInventory() error {
	klog.V(2).Infof("delete inventory task starting (name: %q)", i.Name())
	err := i.InvClient.DeleteInventoryObj(i.InvInfo, i.DryRun)
	// Not found is not error, since this means it was already deleted.
	if apierrors.IsNotFound(err) {
		err = nil
	}
	klog.V(2).Infof("delete inventory task completing (name: %q)", i.Name())
	return err
}

// destroySuccessful returns true when destroy actuation and reconciliation was
// fully successful. When true, it's safe to delete the inventory.
func (i *DeleteOrUpdateInvTask) destroySuccessful(taskContext *taskrunner.TaskContext) bool {
	// if any deletes failed, the Destroy is considered failed
	if len(taskContext.InventoryManager().FailedDeletes()) > 0 {
		return false
	}
	// if any reconciles failed, the Destroy is considered failed
	if len(taskContext.InventoryManager().FailedReconciles()) > 0 {
		return false
	}
	// if any reconciles timed out, the Destroy is considered failed
	if len(taskContext.InventoryManager().TimeoutReconciles()) > 0 {
		return false
	}
	return true
}
