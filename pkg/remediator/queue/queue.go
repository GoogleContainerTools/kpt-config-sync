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
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
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
func GVKNNOf(obj client.Object) GVKNN {
	return GVKNN{
		ID:      core.IDOf(obj),
		Version: obj.GetObjectKind().GroupVersionKind().Version,
	}
}

// Interface is the methods ObjectQueue satisfies.
// See ObjectQueue for method definitions.
type Interface interface {
	Add(obj client.Object)
	Get() (client.Object, bool)
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
		klog.V(4).Infof("Queue already contains object %q with generation %d; ignoring object: %v", gvknn, current.GetGeneration(), obj)
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
	klog.V(2).Infof("Upserting object into queue: %v", obj)
	q.objects[gvknn] = obj
	q.underlying.Add(gvknn)

	if !q.dirty[gvknn] {
		q.dirty[gvknn] = true
		q.cond.Signal()
	}
}

// Retry schedules the object to be requeued using the rate limiter.
func (q *ObjectQueue) Retry(obj client.Object) {
	gvknn := GVKNNOf(obj)
	q.delayer.AddAfter(obj, q.rateLimiter.When(gvknn))
}

// Get blocks until it can return an item to be processed.
//
// Returns the next item to process, and whether the queue has been shut down
// and has no more items to process.
//
// If the queue has been shut down the caller should end their goroutine.
//
// You must call Done with item when you have finished processing it or else the
// item will never be processed again.
func (q *ObjectQueue) Get() (client.Object, bool) {
	q.cond.L.Lock()
	defer q.cond.L.Unlock()

	// This is a yielding block that will allow Add() and Done() to be called
	// while it blocks.
	for q.underlying.Len() == 0 && !q.underlying.ShuttingDown() {
		q.cond.Wait()
	}

	item, shutdown := q.underlying.Get()
	if item == nil || shutdown {
		return nil, shutdown
	}

	gvknn, isID := item.(GVKNN)
	if !isID {
		klog.Warningf("Got non GVKNN from work queue: %v", item)
		q.underlying.Done(item)
		q.rateLimiter.Forget(item)
		return nil, false
	}

	obj := q.objects[gvknn]
	delete(q.dirty, gvknn)
	klog.V(4).Infof("Fetched object for processing: %v", obj)
	return obj.DeepCopyObject().(client.Object), false
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
		klog.V(4).Infof("Leaving dirty object reference in place: %v", q.objects[gvknn])
		q.cond.Signal()
	} else {
		klog.V(2).Infof("Removing clean object reference: %v", q.objects[gvknn])
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
	q.underlying.ShutDown()
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
	return g.oq.Get()
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
