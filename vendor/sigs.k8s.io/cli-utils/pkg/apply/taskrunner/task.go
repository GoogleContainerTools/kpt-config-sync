// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package taskrunner

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/cli-utils/pkg/object"
)

var (
	crdGK = schema.GroupKind{Group: "apiextensions.k8s.io", Kind: "CustomResourceDefinition"}
)

// Task is the interface that must be implemented by
// all tasks that will be executed by the taskrunner.
type Task interface {
	Name() string
	Action() event.ResourceAction
	Identifiers() object.ObjMetadataSet
	Start(*TaskContext)
	StatusUpdate(*TaskContext, object.ObjMetadata)
	Cancel(*TaskContext)
}

// NewWaitTask creates a new wait task where we will wait until
// the resources specifies by ids all meet the specified condition.
func NewWaitTask(name string, ids object.ObjMetadataSet, cond Condition, timeout time.Duration, mapper meta.RESTMapper) *WaitTask {
	return &WaitTask{
		TaskName:  name,
		Ids:       ids,
		Condition: cond,
		Timeout:   timeout,
		Mapper:    mapper,
	}
}

// WaitTask is an implementation of the Task interface that is used
// to wait for a set of resources (identified by a slice of ObjMetadata)
// will all meet the condition specified. It also specifies a timeout
// for how long we are willing to wait for this to happen.
// Unlike other implementations of the Task interface, the wait task
// is handled in a special way to the taskrunner and is a part of the core
// package.
type WaitTask struct {
	// TaskName allows providing a name for the task.
	TaskName string
	// Ids is the full list of resources that we are waiting for.
	Ids object.ObjMetadataSet
	// Condition defines the status we want all resources to reach
	Condition Condition
	// Timeout defines how long we are willing to wait for the condition
	// to be met.
	Timeout time.Duration
	// Mapper is the RESTMapper to update after CRDs have been reconciled
	Mapper meta.RESTMapper
	// cancelFunc is a function that will cancel the timeout timer
	// on the task.
	cancelFunc context.CancelFunc
	// pending is the set of resources that we are still waiting for.
	pending object.ObjMetadataSet
	// failed is the set of resources that we are waiting for, but is considered
	// failed, i.e. unlikely to successfully reconcile.
	failed object.ObjMetadataSet
	// mu protects the pending ObjMetadataSet
	mu sync.RWMutex
}

func (w *WaitTask) Name() string {
	return w.TaskName
}

func (w *WaitTask) Action() event.ResourceAction {
	return event.WaitAction
}

func (w *WaitTask) Identifiers() object.ObjMetadataSet {
	return w.Ids
}

// Start kicks off the task. For the wait task, this just means
// setting up the timeout timer.
func (w *WaitTask) Start(taskContext *TaskContext) {
	klog.V(2).Infof("wait task starting (name: %q, objects: %d)",
		w.Name(), len(w.Ids))

	// TODO: inherit context from task runner, passed through the TaskContext
	ctx := context.Background()

	// use a context wrapper to handle complete/cancel/timeout
	if w.Timeout > 0 {
		ctx, w.cancelFunc = context.WithTimeout(ctx, w.Timeout)
	} else {
		ctx, w.cancelFunc = context.WithCancel(ctx)
	}

	w.startInner(taskContext)

	// A goroutine to handle ending the WaitTask.
	go func() {
		// Block until complete/cancel/timeout
		<-ctx.Done()
		// Err is always non-nil when Done channel is closed.
		err := ctx.Err()

		klog.V(2).Infof("wait task completing (name: %q,): %v", w.TaskName, err)

		switch err {
		case context.Canceled:
			// happy path - cancelled or completed (not considered an error)
		case context.DeadlineExceeded:
			// timed out
			w.sendTimeoutEvents(taskContext)
		}

		// Update RESTMapper to pick up new custom resource types
		w.updateRESTMapper(taskContext)

		// Done here. signal completion to the task runner
		taskContext.TaskChannel() <- TaskResult{}
	}()
}

func (w *WaitTask) sendEvent(taskContext *TaskContext, id object.ObjMetadata, status event.WaitEventStatus) {
	taskContext.SendEvent(event.Event{
		Type: event.WaitType,
		WaitEvent: event.WaitEvent{
			GroupName:  w.Name(),
			Identifier: id,
			Status:     status,
		},
	})
}

// startInner sends initial pending, skipped, an reconciled events.
// If all objects are reconciled or skipped, cancelFunc is called.
// The pending set is write locked during execution of startInner.
func (w *WaitTask) startInner(taskContext *TaskContext) {
	w.mu.Lock()
	defer w.mu.Unlock()

	pending := object.ObjMetadataSet{}
	for _, id := range w.Ids {
		switch {
		case w.skipped(taskContext, id):
			err := taskContext.InventoryManager().SetSkippedReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as skipped reconcile: %v", err)
			}
			w.sendEvent(taskContext, id, event.ReconcileSkipped)
		case w.changedUID(taskContext, id):
			// replaced
			w.handleChangedUID(taskContext, id)
		case w.reconciledByID(taskContext, id):
			err := taskContext.InventoryManager().SetSuccessfulReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as successful reconcile: %v", err)
			}
			w.sendEvent(taskContext, id, event.ReconcileSuccessful)
		default:
			err := taskContext.InventoryManager().SetPendingReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as pending reconcile: %v", err)
			}
			pending = append(pending, id)
			w.sendEvent(taskContext, id, event.ReconcilePending)
		}
	}
	w.pending = pending

	if len(pending) == 0 {
		// all reconciled - clear pending and exit
		klog.V(3).Infof("all objects reconciled or skipped (name: %q)", w.TaskName)
		w.cancelFunc()
	}
}

// sendTimeoutEvents sends a timeout event for every remaining pending object
// The pending set is read locked during execution of sendTimeoutEvents.
func (w *WaitTask) sendTimeoutEvents(taskContext *TaskContext) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for _, id := range w.pending {
		err := taskContext.InventoryManager().SetTimeoutReconcile(id)
		if err != nil {
			// Object never applied or deleted!
			klog.Errorf("Failed to mark object as pending reconcile: %v", err)
		}
		w.sendEvent(taskContext, id, event.ReconcileTimeout)
	}
}

// reconciledByID checks whether the condition set in the task is currently met
// for the specified object given the status of resource in the cache.
func (w *WaitTask) reconciledByID(taskContext *TaskContext, id object.ObjMetadata) bool {
	return conditionMet(taskContext, object.ObjMetadataSet{id}, w.Condition)
}

// skipped returns true if the object failed or was skipped by a preceding
// apply/delete/prune task.
func (w *WaitTask) skipped(taskContext *TaskContext, id object.ObjMetadata) bool {
	im := taskContext.InventoryManager()
	if w.Condition == AllCurrent &&
		im.IsFailedApply(id) || im.IsSkippedApply(id) {
		return true
	}
	if w.Condition == AllNotFound &&
		im.IsFailedDelete(id) || im.IsSkippedDelete(id) {
		return true
	}
	return false
}

// failedByID returns true if the resource is failed.
func (w *WaitTask) failedByID(taskContext *TaskContext, id object.ObjMetadata) bool {
	cached := taskContext.ResourceCache().Get(id)
	return cached.Status == status.FailedStatus
}

// changedUID returns true if the UID of the object has changed since it was
// applied or deleted. This indicates that the object was deleted and recreated.
func (w *WaitTask) changedUID(taskContext *TaskContext, id object.ObjMetadata) bool {
	var oldUID, newUID types.UID

	// Get the uid from the ApplyTask/PruneTask
	taskObj, found := taskContext.InventoryManager().ObjectStatus(id)
	if !found {
		klog.Errorf("Unknown object UID from InventoryManager: %v", id)
		return false
	}
	oldUID = taskObj.UID
	if oldUID == "" {
		// All objects should have been given a UID by the apiserver
		klog.Errorf("Empty object UID from InventoryManager: %v", id)
		return false
	}

	// Get the uid from the StatusPoller
	pollerObj := taskContext.ResourceCache().Get(id)
	if pollerObj.Resource == nil {
		switch pollerObj.Status {
		case status.UnknownStatus:
			// Resource is expected to be nil when Unknown.
		case status.NotFoundStatus:
			// Resource is expected to be nil when NotFound.
			// K8s DELETE API doesn't always return an object.
		default:
			// For all other statuses, nil Resource is probably a bug.
			klog.Errorf("Unknown object UID from ResourceCache (status: %v): %v", pollerObj.Status, id)
		}
		return false
	}
	newUID = pollerObj.Resource.GetUID()
	if newUID == "" {
		// All objects should have been given a UID by the apiserver
		klog.Errorf("Empty object UID from ResourceCache (status: %v): %v", pollerObj.Status, id)
		return false
	}

	return (oldUID != newUID)
}

// handleChangedUID updates the object status and sends an event
func (w *WaitTask) handleChangedUID(taskContext *TaskContext, id object.ObjMetadata) {
	switch w.Condition {
	case AllNotFound:
		// Object recreated by another actor after deletion.
		// Treat as success.
		klog.Infof("UID change detected: deleted object have been recreated: marking reconcile successful: %v", id)
		err := taskContext.InventoryManager().SetSuccessfulReconcile(id)
		if err != nil {
			// Object never applied or deleted!
			klog.Errorf("Failed to mark object as successful reconcile: %v", err)
		}
		w.sendEvent(taskContext, id, event.ReconcileSuccessful)
	case AllCurrent:
		// Object deleted and recreated by another actor after apply.
		// Treat as failure (unverifiable).
		klog.Infof("UID change detected: applied object has been deleted and recreated: marking reconcile failed: %v", id)
		err := taskContext.InventoryManager().SetFailedReconcile(id)
		if err != nil {
			// Object never applied or deleted!
			klog.Errorf("Failed to mark object as failed reconcile: %v", err)
		}
		w.sendEvent(taskContext, id, event.ReconcileFailed)
	default:
		panic(fmt.Sprintf("Invalid wait condition: %v", w.Condition))
	}
}

// Cancel exits early with a timeout error
func (w *WaitTask) Cancel(_ *TaskContext) {
	w.cancelFunc()
}

// StatusUpdate records objects status updates and sends WaitEvents.
// If all objects are reconciled or skipped, cancelFunc is called.
// The pending set is write locked during execution of StatusUpdate.
func (w *WaitTask) StatusUpdate(taskContext *TaskContext, id object.ObjMetadata) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if klog.V(5).Enabled() {
		status := taskContext.ResourceCache().Get(id).Status
		klog.Infof("status update (object: %q, status: %q)", id, status)
	}

	switch {
	case w.pending.Contains(id):
		switch {
		case w.changedUID(taskContext, id):
			// replaced
			w.handleChangedUID(taskContext, id)
			w.pending = w.pending.Remove(id)
		case w.reconciledByID(taskContext, id):
			// reconciled - remove from pending & send event
			err := taskContext.InventoryManager().SetSuccessfulReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as successful reconcile: %v", err)
			}
			w.pending = w.pending.Remove(id)
			w.sendEvent(taskContext, id, event.ReconcileSuccessful)
		case w.failedByID(taskContext, id):
			// failed - remove from pending & send event
			err := taskContext.InventoryManager().SetFailedReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as failed reconcile: %v", err)
			}
			w.pending = w.pending.Remove(id)
			w.failed = append(w.failed, id)
			w.sendEvent(taskContext, id, event.ReconcileFailed)
		default:
			// can't be all reconciled now, so don't bother checking
			return
		}
	case !w.Ids.Contains(id):
		// not in wait group - ignore
		return
	case w.skipped(taskContext, id):
		// skipped - ignore
		return
	case w.failed.Contains(id):
		// If a failed resource becomes current before other
		// resources have completed/timed out, we consider it
		// current.
		if w.reconciledByID(taskContext, id) {
			// reconciled - remove from pending & send event
			err := taskContext.InventoryManager().SetSuccessfulReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as successful reconcile: %v", err)
			}
			w.failed = w.failed.Remove(id)
			w.sendEvent(taskContext, id, event.ReconcileSuccessful)
		} else if !w.failedByID(taskContext, id) {
			// If a resource is no longer reported as Failed and is not Reconciled,
			// they should just go back to InProgress.
			err := taskContext.InventoryManager().SetPendingReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as pending reconcile: %v", err)
			}
			w.failed = w.failed.Remove(id)
			w.pending = append(w.pending, id)
			w.sendEvent(taskContext, id, event.ReconcilePending)
		}
		// can't be all reconciled, so don't bother checking. A failed
		// resource doesn't prevent a WaitTask from completing, so there
		// must be at least one InProgress resource we are still waiting for.
		return
	default:
		// reconciled - check if unreconciled
		if !w.reconciledByID(taskContext, id) {
			// unreconciled - add to pending & send event
			err := taskContext.InventoryManager().SetPendingReconcile(id)
			if err != nil {
				// Object never applied or deleted!
				klog.Errorf("Failed to mark object as pending reconcile: %v", err)
			}
			w.pending = append(w.pending, id)
			w.sendEvent(taskContext, id, event.ReconcilePending)
			// can't be all reconciled now, so don't bother checking
			return
		}
	}

	// If we no longer have any pending resources, the WaitTask
	// can be completed.
	if len(w.pending) == 0 {
		// all reconciled, so exit
		klog.V(3).Infof("all objects reconciled or skipped (name: %q)", w.TaskName)
		w.cancelFunc()
	}
}

// updateRESTMapper resets the RESTMapper if CRDs were applied, so that new
// resource types can be applied by subsequent tasks.
// TODO: find a way to add/remove mappers without resetting the entire mapper
// Resetting the mapper requires all CRDs to be queried again.
func (w *WaitTask) updateRESTMapper(taskContext *TaskContext) {
	foundCRD := false
	for _, id := range w.Ids {
		if id.GroupKind == crdGK && !w.skipped(taskContext, id) {
			foundCRD = true
			break
		}
	}
	if !foundCRD {
		// no update required
		return
	}

	klog.V(5).Infof("resetting RESTMapper")
	meta.MaybeResetRESTMapper(w.Mapper)
}
