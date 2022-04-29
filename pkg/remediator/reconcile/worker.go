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
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Worker pulls objects from a work queue and passes them to its reconciler for
// remediation.
type Worker struct {
	objectQueue queue.Interface
	reconciler  reconcilerInterface
}

// NewWorker returns a new Worker for the given queue and declared resources.
func NewWorker(scope declared.Scope, syncName string, a syncerreconcile.Applier, q *queue.ObjectQueue, d *declared.Resources) *Worker {
	return &Worker{
		objectQueue: q,
		reconciler:  newReconciler(scope, syncName, a, d),
	}
}

// Run starts the Worker pulling objects from its queue for remediation. This
// call blocks until the given context is cancelled.
func (w *Worker) Run(ctx context.Context) {
	wait.UntilWithContext(ctx, func(ctx context.Context) {
		for w.processNextObject(ctx) {
		}
	}, 1*time.Second)
	w.objectQueue.ShutDown()
}

func (w *Worker) processNextObject(ctx context.Context) bool {
	obj, shutdown := w.objectQueue.Get()
	if shutdown {
		return false
	}
	if obj == nil {
		return true
	}

	defer w.objectQueue.Done(obj)
	return w.process(ctx, obj)
}

func (w *Worker) process(ctx context.Context, obj client.Object) bool {
	var toRemediate client.Object
	if queue.WasDeleted(ctx, obj) {
		// Passing a nil Object to the reconciler signals that the accompanying ID
		// is for an Object that was deleted.
		toRemediate = nil
	} else {
		toRemediate = obj
	}

	now := time.Now()
	err := w.reconciler.Remediate(ctx, core.IDOf(obj), toRemediate)
	metrics.RecordRemediateDuration(ctx, metrics.StatusTagKey(err), obj.GetObjectKind().GroupVersionKind(), now)
	if err != nil {
		// To debug the set of events we've missed, you may need to comment out this
		// block. Specifically, this makes things smooth for production, but can
		// hide bugs (for example, if we don't properly process delete events).
		if err.Code() == syncerclient.ResourceConflictCode {
			// This means our cached version of the object isn't the same as the one
			// on the cluster. We need to refresh the cached version.
			metrics.RecordResourceConflict(ctx, obj.GetObjectKind().GroupVersionKind())
			err := w.refresh(ctx, obj)
			if err != nil {
				klog.Errorf("Worker unable to update cached version of %q: %v", core.IDOf(obj), err)
			}
		}

		klog.Errorf("Worker received an error while reconciling %q: %v", core.IDOf(obj), err)
		w.objectQueue.Retry(obj)

		return false
	}

	klog.V(3).Infof("Worker reconciled %q", core.IDOf(obj))
	w.objectQueue.Forget(obj)
	return true
}

// refresh updates the cached version of the object.
func (w *Worker) refresh(ctx context.Context, o client.Object) status.Error {
	c := w.reconciler.GetClient()

	// Try to get an updated version of the object from the cluster.
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(o.GetObjectKind().GroupVersionKind())
	err := c.Get(ctx, client.ObjectKey{Name: o.GetName(), Namespace: o.GetNamespace()}, u)

	switch {
	case apierrors.IsNotFound(err):
		// The object no longer exists on the cluster, so mark it deleted.
		w.objectQueue.Add(queue.MarkDeleted(ctx, o))
	case err != nil:
		// We encountered some other error that we don't know how to solve, so
		// surface it.
		return status.APIServerError(err, "failed to get updated object for worker cache", o)
	default:
		// Update the cached version of the resource.
		w.objectQueue.Add(u)
	}
	return nil
}
