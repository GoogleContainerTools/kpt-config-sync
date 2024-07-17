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
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/remediator/conflict"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Worker pulls objects from a work queue and passes them to its reconciler for
// remediation.
type Worker struct {
	objectQueue queue.Interface
	reconciler  reconcilerInterface
}

// NewWorker returns a new Worker for the given queue and declared resources.
func NewWorker(scope declared.Scope, syncName string, a syncerreconcile.Applier,
	q *queue.ObjectQueue, d *declared.Resources, ch conflict.Handler, fh fight.Handler) *Worker {
	return &Worker{
		objectQueue: q,
		reconciler:  newReconciler(scope, syncName, a, d, ch, fh),
	}
}

// Run starts the Worker pulling objects from its queue for remediation. This
// call blocks until the given context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	klog.V(1).Info("Worker starting...")
	ctx, cancel := context.WithCancel(ctx)
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		// Attempt to drain the queue
		for {
			if err := w.processNextObject(ctx); err != nil {
				if err == queue.ErrShutdown {
					klog.Infof("Worker stopping: %v", err)
					cancel()
					return
				}
				if err == context.Canceled || err == context.DeadlineExceeded {
					klog.Infof("Worker stopping: %v", err)
					return
				}
				klog.Errorf("Worker error (retry scheduled): %v", err)
				return
			}
		}
		// Once an attempt has been made for every object in the queue,
		// sleep for ~1s before retrying.
	}, 1*time.Second)
	klog.V(3).Info("Worker stopped")
}

// processNextObject remediates object received from the queue.
// Returns an error if the context is cancelled, the queue is shut down, or
// processing the item failed.
func (w *Worker) processNextObject(ctx context.Context) error {
	klog.V(3).Info("Worker waiting for new object...")
	obj, err := w.objectQueue.Get(ctx)
	if err != nil {
		return err
	}
	if obj == nil {
		return nil
	}
	defer w.objectQueue.Done(obj)
	klog.V(3).Infof("Worker processing object %q (generation: %d)",
		queue.GVKNNOf(obj), obj.GetGeneration())
	return w.process(ctx, obj)
}

func (w *Worker) process(ctx context.Context, obj client.Object) error {
	id := core.IDOf(obj)
	var toRemediate client.Object
	if queue.WasDeleted(ctx, obj) {
		// Unwrap deleted object
		prevObj := obj.(*queue.Deleted).Object
		// Get object from cluster to confirm deletion.
		// This is required because delete events always include the previous
		// object state, even if the object wasn't actually deleted, which can
		// happen when a selected label is removed.
		latestObj, err := w.getObject(ctx, prevObj)
		if err != nil {
			// Failed to confirm object state. Enqueue for retry.
			w.objectQueue.Retry(obj)
			return fmt.Errorf("failed to remediate %q: %w", id, err)
		}
		if queue.WasDeleted(ctx, latestObj) {
			// Confirmed not on the server.
			// Passing a nil Object to the reconciler signals that the
			// accompanying ID is for an Object that was deleted.
			toRemediate = nil
		} else {
			// Not actually deleted, or if it was then it's since been recreated.
			// Pass the latest Object state to the reconciler to handle as if
			// in response to an update event.
			toRemediate = latestObj
		}
	} else {
		toRemediate = obj
	}

	err := w.reconciler.Remediate(ctx, id, toRemediate)
	if err != nil {
		// To debug the set of events we've missed, you may need to comment out this
		// block. Specifically, this makes things smooth for production, but can
		// hide bugs (for example, if we don't properly process delete events).
		if err.Code() == syncerclient.ResourceConflictCode {
			// This means our cached version of the object isn't the same as the one
			// on the cluster. We need to refresh the cached version.
			if refreshErr := w.refresh(ctx, obj); refreshErr != nil {
				klog.Errorf("Worker unable to update cached version of %q: %v", id, refreshErr)
			}
		}
		w.objectQueue.Retry(obj)
		return fmt.Errorf("failed to remediate %q: %w", id, err)
	}

	klog.V(3).Infof("Worker reconciled %q", id)
	w.objectQueue.Forget(obj)
	return nil
}

// refresh updates the cached version of the object.
func (w *Worker) refresh(ctx context.Context, obj client.Object) status.Error {
	obj, err := w.getObject(ctx, obj)
	if err != nil {
		return err
	}
	// Enqueue object for remediation
	w.objectQueue.Add(obj)
	return nil
}

// getObject updates the object from the server.
// Wraps the supplied object with MarkDeleted, if NotFound.
func (w *Worker) getObject(ctx context.Context, obj client.Object) (client.Object, status.Error) {
	uObj := &unstructured.Unstructured{}
	uObj.SetGroupVersionKind(obj.GetObjectKind().GroupVersionKind())
	err := w.reconciler.GetClient().Get(ctx, client.ObjectKeyFromObject(obj), uObj)
	if err != nil {
		// If not found, wrap for processing as a deleted object
		if apierrors.IsNotFound(err) {
			return queue.MarkDeleted(ctx, obj), nil
		}
		// Surface any other errors
		return uObj, status.APIServerError(err, "failed to get updated object for worker cache", obj)
	}
	return uObj, nil
}
