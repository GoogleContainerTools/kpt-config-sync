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
	"errors"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GVKNN adds Version to core.ID to make it suitable for getting an object from
// a cluster into an *unstructured.Unstructured.
type GVKNN struct {
	core.ID
	Version string
}

// GroupVersionKind returns the GVK contained in this GVKNN.
func (gvknn GVKNN) GroupVersionKind() schema.GroupVersionKind {
	return gvknn.GroupKind.WithVersion(gvknn.Version)
}

// GVKNNOf converts an Object to its GVKNN.
//
// Panics if the GroupKind is not set and not registered in core.Scheme.
//
// Deprecated: Typed objects should not include GroupKind! Use LookupGKVNN
// instead, and handle the error, instead of panicking or returning a GKVNN with
// empty GroupVersionKind.
func GVKNNOf(obj client.Object) GVKNN {
	// Unwrap deleted objects
	if dObj, ok := obj.(*Deleted); ok {
		obj = dObj.Object
	}
	gvk, err := kinds.Lookup(obj, core.Scheme)
	if err != nil {
		klog.Fatalf("GVKNNOf: Failed to lookup GroupVersionKind of %T: %v", obj, err)
	}
	return GVKNN{
		ID: core.ID{
			GroupKind: gvk.GroupKind(),
			ObjectKey: client.ObjectKeyFromObject(obj),
		},
		Version: gvk.Version,
	}
}

// IDOf converts an Object to its ID.
//
// Panics if the GroupKind is not set and not registered in core.Scheme.
//
// Deprecated: Typed objects should not include GroupKind! Use LookupID instead,
// and handle the error, instead of panicking or returning an ID with empty
// GroupKind.
func IDOf(obj client.Object) core.ID {
	// Unwrap deleted objects
	if dObj, ok := obj.(*Deleted); ok {
		obj = dObj.Object
	}
	return core.IDOf(obj)
}

// ErrShutdown is returned by `ObjectQueue.Get` if the queue is shut down
// before an object is available/added.
var ErrShutdown = errors.New("object queue shutting down")

// Interface is the methods ObjectQueue satisfies.
// See ObjectQueue for method definitions.
type Interface interface {
	Add(obj client.Object)
	Get(context.Context) (client.Object, error)
	Done(obj client.Object)
	Forget(obj client.Object)
	Retry(obj client.Object)
	ShutDown()
}

// ObjectQueue is a wrapper around a workqueue.Interface for use with declared
// resources. It deduplicates work items by their GVKNN.
// NOTE: This was originally designed to wrap a DelayingInterface, but we have
// had to copy a lot of that logic here. At some point it may make sense to
// remove the underlying workqueue.Interface and just consolidate copied logic
// here.
type ObjectQueue struct {
	// cond is a locking condition which allows us to lock all mutating calls but
	// also allow any call to yield the lock safely (specifically for Get).
	cond *sync.Cond
	// rateLimiter enables the ObjectQueue to support rate-limited retries.
	rateLimiter workqueue.RateLimiter
	// delayer is a wrapper around the ObjectQueue which supports delayed Adds.
	delayer workqueue.DelayingInterface
	// underlying is the workqueue that contains work item keys so that it can
	// maintain the order in which those items should be worked on.
	underlying workqueue.Interface
	// objects is a map of actual work items which need to be processed.
	objects map[GVKNN]client.Object
	// dirty is a map of object keys which will need to be reprocessed even if
	// they are currently being processed. This is explained further in Add().
	dirty map[GVKNN]bool
}

// New creates a new work queue for use in signalling objects that may need
// remediation.
func New(name string) *ObjectQueue {
	oq := &ObjectQueue{
		cond:        sync.NewCond(&sync.Mutex{}),
		rateLimiter: workqueue.DefaultControllerRateLimiter(),
		underlying:  workqueue.NewNamed(name),
		objects:     map[GVKNN]client.Object{},
		dirty:       map[GVKNN]bool{},
	}
	oq.delayer = delayingWrap(oq, name)
	return oq
}

// Add marks the object as needing processing unless the object is already in
// the queue AND the existing object is more current than the new one.
func (q *ObjectQueue) Add(obj client.Object) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	gvknn := GVKNNOf(obj)

	// Generation is not incremented when metadata is changed. Therefore if
	// generation is equal, we default to accepting the new object as it may have
	// new labels or annotations or other metadata.
	if current, ok := q.objects[gvknn]; ok && current.GetGeneration() > obj.GetGeneration() {
		klog.V(1).Infof("ObjectQueue.Add: already contains object %q with generation %d; ignoring object with generation %d",
			gvknn, current.GetGeneration(), obj.GetGeneration())
		return
	}

	// It is possible that a reconciler has already pulled the object for this
	// GVKNN out of the queue and is actively processing it. In that case, we
	// need to mark it dirty here so that it gets re-processed. Eg:
	// 1. q.objects contains generation 1 of a resource.
	// 2. A reconciler pulls gen1 out of the queue to process.
	// 3. The gvknn is no longer marked dirty (see Get() below).
	// 3. Another process/user updates the resource in parallel.
	// 4. The API server notifies the watcher which calls Add() with gen2 of the resource.
	// 5. We insert gen2 and re-mark the gvknn as dirty.
	// 6. The reconciler finishes processing gen1 of the resource and calls Done().
	// 7. Since the gvknn is still marked dirty, we leave the resource in q.objects.
	// 8. Eventually a reconciler pulls gen2 of the resource out of the queue for processing.
	// 9. The gvknn is no longer marked dirty.
	// 10. The reconciler finishes processing gen2 of the resource and calls Done().
	// 11. Since the gvknn is not marked dirty, we remove the resource from q.objects.
	klog.V(2).Infof("ObjectQueue.Add: %v (generation: %d)",
		gvknn, obj.GetGeneration())
	q.objects[gvknn] = obj
	q.underlying.Add(gvknn)

	if !q.dirty[gvknn] {
		q.dirty[gvknn] = true
		// Signal the Get Wait to continue, to detect the new object
		q.cond.Signal()
	}
}

// Retry schedules the object to be requeued using the rate limiter.
func (q *ObjectQueue) Retry(obj client.Object) {
	gvknn := GVKNNOf(obj)
	q.delayer.AddAfter(obj, q.rateLimiter.When(gvknn))
}

// Get blocks until one of the following conditions:
// A) An item is ready to be processed
// B) The context is cancelled or times out
// C) The queue is shutting down
//
// Returns the next item to process, and either a `context.Canceled`,
// `context.DeadlineExceeded`, or `queue.ErrShutdown`.
//
// If the queue has been shut down the caller should end their goroutine.
//
// You must call Done with item when you have finished processing it or else the
// item will never be processed again.
func (q *ObjectQueue) Get(ctx context.Context) (client.Object, error) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	// This background thread converts a done channel into a condition signal.
	// This is required because the underlying workqueue.Interface doesn't use
	// channels to communicate.
	innerCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-innerCtx.Done():
			// Stop waiting for ctx to be cancelled
		case <-ctx.Done():
			// Signal the Get Wait to continue, to detect the context error
			klog.V(3).Infof("ObjectQueue.Get interrupted: %v", ctx.Err())
			q.cond.Signal()
		}
	}()

	// This is a yielding block that will allow Add() and Done() to be called
	// while it blocks.
	for q.underlying.Len() == 0 {
		if err := ctx.Err(); err != nil {
			klog.V(3).Infof("ObjectQueue.Get returning: %v", err)
			return nil, err
		}
		if q.underlying.ShuttingDown() {
			klog.V(3).Info("ObjectQueue.Get returning: Shutting Down")
			return nil, ErrShutdown
		}
		klog.V(3).Info("ObjectQueue.Get waiting: Empty Queue")
		q.cond.Wait()
	}

	// Stop waiting for ctx to be cancelled
	cancel()

	// Because length > 0 and nothing else can remove from it while locked,
	// Get should return immediately.
	item, shutdown := q.underlying.Get()
	if shutdown {
		return nil, ErrShutdown
	}
	if item == nil {
		return nil, nil
	}

	gvknn, isID := item.(GVKNN)
	if !isID {
		klog.Warningf("ObjectQueue.Get: expected GVKNN from work queue, but found %T: %v", item, item)
		q.underlying.Done(item)
		q.rateLimiter.Forget(item)
		return nil, nil
	}

	obj := q.objects[gvknn]
	delete(q.dirty, gvknn)
	klog.V(2).Infof("ObjectQueue.Get: returning object: %v (generation: %d)",
		gvknn, obj.GetGeneration())
	return kinds.ObjectAsClientObject(obj.DeepCopyObject())
}

// Done marks item as done processing, and if it has been marked as dirty again
// while it was being processed, it will be re-added to the queue for
// re-processing.
func (q *ObjectQueue) Done(obj client.Object) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	gvknn := GVKNNOf(obj)
	q.underlying.Done(gvknn)

	if q.dirty[gvknn] {
		klog.V(3).Infof("ObjectQueue.Done: retaining object for retry: %v (generation: %d)",
			gvknn, obj.GetGeneration())
		// Signal the Get Wait to continue, to detect the new object
		q.cond.Signal()
	} else {
		klog.V(3).Infof("ObjectQueue.Done: removing object from queue: %v (generation: %d)",
			gvknn, obj.GetGeneration())
		delete(q.objects, gvknn)
	}
}

// Forget is a convenience method that allows callers to directly tell the
// RateLimitingInterface to forget a specific client.Object.
func (q *ObjectQueue) Forget(obj client.Object) {
	gvknn := GVKNNOf(obj)
	q.rateLimiter.Forget(gvknn)
}

// Len returns the length of the underlying queue.
func (q *ObjectQueue) Len() int {
	return q.underlying.Len()
}

// ShutDown shuts down the object queue.
func (q *ObjectQueue) ShutDown() {
	klog.V(1).Info("ObjectQueue.ShutDown()")
	q.underlying.ShutDown()
	// Signal the Get Wait to continue, to detect the shutdown
	q.cond.Signal()
}

// ShuttingDown returns true if the object queue is shutting down.
func (q *ObjectQueue) ShuttingDown() bool {
	return q.underlying.ShuttingDown()
}

// delayingWrap returns the given ObjectQueue wrapped in a DelayingInterface to
// enable rate-limited retries.
func delayingWrap(oq *ObjectQueue, name string) workqueue.DelayingInterface {
	gw := &genericWrapper{oq}
	return workqueue.NewDelayingQueueWithCustomQueue(gw, name)
}

// genericWrapper is an internal wrapper that allows us to pass an ObjectQueue
// to a DelayingInterface to enable rate-limited retries. It uses unsafe type
// conversion because it should only ever be used in this file and so we know
// that all of the item interfaces are actually client.Objects.
type genericWrapper struct {
	oq *ObjectQueue
}

var _ workqueue.Interface = &genericWrapper{}

// Add implements workqueue.Interface.
func (g *genericWrapper) Add(item interface{}) {
	g.oq.Add(item.(client.Object))
}

// Get implements workqueue.Interface.
func (g *genericWrapper) Get() (item interface{}, shutdown bool) {
	obj, err := g.oq.Get(context.Background())
	return obj, (err != nil)
}

// Done implements workqueue.Interface.
func (g *genericWrapper) Done(item interface{}) {
	g.oq.Done(item.(client.Object))
}

// Len implements workqueue.Interface.
func (g *genericWrapper) Len() int {
	return g.oq.Len()
}

// ShutDown implements workqueue.Interface.
func (g *genericWrapper) ShutDown() {
	g.oq.ShutDown()
}

// ShuttingDown implements workqueue.Interface.
func (g *genericWrapper) ShuttingDown() bool {
	return g.oq.ShuttingDown()
}

// ShutDownWithDrain implements workqueue.Interface.
func (g *genericWrapper) ShutDownWithDrain() {
	g.oq.ShutDown()
}
