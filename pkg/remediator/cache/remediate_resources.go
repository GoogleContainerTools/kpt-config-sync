// Copyright 2023 Google LLC
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

package cache

import (
	"sync"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeletedResourceVersion marks the deleted object's RV as `deleted`.
// The ResourceVersion is used to determine whether to skip the next remediation.
// If the object was deleted during the remediation, the ResourceVersion of the
// remediated object is not available, so mark it as `deleted`.
const DeletedResourceVersion = "deleted"

// RemediateResources is a threadsafe cache shared by both Syncer and Remediator.
// Syncer starts watchers for declared resources, and each watcher adds objects to the map.
// Remediator checks the objs map periodically and enqueues the object for remediating.
type RemediateResources struct {
	// mux is a RMMutex because the cache is read more often than writen.
	mux sync.RWMutex
	// objs maps from the GVKK to the object. GVKK is enqueued by the Remediator, and object is used to correct the drift.
	objs map[core.ID]client.Object
	// objsQueue stores remediate resources that are waiting for processing.
	objsQueue map[core.ID]*remediateState
}

// remediateState represents the remediate state of an object in the queue.
// It is used to determine the next remediate process.
type remediateState struct {
	// done represents if the enqueued object finishes the current remediation
	// If complete, it needs to be requeued as the remeidate request.
	// Otherwise, it stays in the queue and waits to be processed.
	done bool

	// remediatedRV represents the object's ResourceVersion after the remediation.
	// If the object is deleted, it sets the value to `deleted`.
	// This field determines whether the next remediation of the same object should be skipped.
	remediatedRV string
}

// NewRemediateResources initializes the two maps.
func NewRemediateResources() *RemediateResources {
	return &RemediateResources{
		objs:      make(map[core.ID]client.Object),
		objsQueue: make(map[core.ID]*remediateState),
	}
}

// GetObjs returns a copy of the objects map.
func (r *RemediateResources) GetObjs() map[core.ID]client.Object {
	r.mux.Lock()
	defer r.mux.Unlock()

	mapCopy := make(map[core.ID]client.Object, len(r.objs))
	for id, obj := range r.objs {
		mapCopy[id] = obj
	}
	return mapCopy
}

// AddObj adds the object to the map.
// It is invoked when an object needs to be remediated.
// This function doesn't add the object to `objsQueue` because the Remediator
// will enqueue the request and put it to `objsQueue`.
func (r *RemediateResources) AddObj(obj client.Object) {
	r.mux.Lock()
	defer r.mux.Unlock()
	id := core.IDOf(obj)
	klog.V(2).Infof("ObjectQueue.Add: %v (generation: %d, resourceversion: %s)",
		id, obj.GetGeneration(), obj.GetResourceVersion())
	r.objs[id] = obj
}

// RemoveObj removes the object from the map when the object is remediated.
// It removes the object from both `objs` and `objsQueue`.
func (r *RemediateResources) RemoveObj(id core.ID) {
	r.mux.Lock()
	defer r.mux.Unlock()
	klog.V(2).Infof("ObjectQueue.Remove: %v", id)
	delete(r.objs, id)
	delete(r.objsQueue, id)
}

// EnqueueObj returns whether to enqueue the given ID as a remediate request.
// It only enqueues if the object is not in the queue, or the object hasn't
// finished the remediation.
func (r *RemediateResources) EnqueueObj(obj client.Object) bool {
	r.mux.Lock()
	defer r.mux.Unlock()
	id := core.IDOf(obj)
	state, found := r.objsQueue[id]
	if found && !state.done {
		klog.V(2).Infof("wait for remediation of %v to complete before re-enqueue it", id)
		return false
	}
	if !found {
		r.objsQueue[id] = &remediateState{}
	} else {
		r.objsQueue[id].done = false
	}
	klog.V(2).Infof("ObjectQueue.Enqueue: %v, (generation: %d, resourceversion: %s)",
		id, obj.GetGeneration(), obj.GetResourceVersion())
	return true
}

// DoneObj marks the object remediation is done.
// If it is not deleted from the map, it should be requeued.
func (r *RemediateResources) DoneObj(id core.ID, rv ...string) {
	r.mux.Lock()
	defer r.mux.Unlock()
	if _, found := r.objsQueue[id]; !found {
		klog.Errorf("object %v is dequeued before done, resourceversion: %s",
			id, rv)
		return
	}
	r.objsQueue[id].done = true
	if len(rv) > 1 {
		klog.Errorf("at most one ResourceVersion is allowed")
		return
	}
	if len(rv) == 1 {
		r.objsQueue[id].remediatedRV = rv[0]
	}
	klog.V(2).Infof("ObjectQueue.DoneObj: %v, resourceversion: %s", id, rv)
}

// LastRemediateRV returns the ResourceVersion from the last remediation.
// It is used to determine whether to skip the next remediation for the same object.
func (r *RemediateResources) LastRemediateRV(id core.ID) string {
	r.mux.Lock()
	defer r.mux.Unlock()
	state, found := r.objsQueue[id]
	if !found {
		klog.Errorf("object %v is dequeued before done", id)
		return ""
	}
	return state.remediatedRV
}

// ClearAll removes all cached resources, including both the object map and the queue.
func (r *RemediateResources) ClearAll() {
	r.mux.Lock()
	defer r.mux.Unlock()
	clear(r.objs)
	clear(r.objsQueue)
}
