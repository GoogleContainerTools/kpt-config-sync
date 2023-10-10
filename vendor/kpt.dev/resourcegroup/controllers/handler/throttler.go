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

package handler

import (
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Throttler only pushes a request to ResourceGroup work queue
// when there is no same event in the queue.
// It works with only GenericEvent and no-op for other events.
type Throttler struct {
	// lock is for thread safe read/write on mapping
	lock sync.RWMutex

	// mapping records which events are to be pushed to the queue.
	// If an incoming event can be found in mapping, it is ignored.
	mapping map[types.NamespacedName]struct{}

	// duration is the time duration that the event
	// is kept in mapping
	duration time.Duration
}

// NewThrottler returns an instance of Throttler
func NewThrottler(d time.Duration) *Throttler {
	return &Throttler{
		lock:     sync.RWMutex{},
		mapping:  make(map[types.NamespacedName]struct{}),
		duration: d,
	}
}

// Create implements EventHandler. All create events are ignored.
func (e *Throttler) Create(event.CreateEvent, workqueue.RateLimitingInterface) {
}

// Update implements EventHandler. All update events are ignored.
func (e *Throttler) Update(event.UpdateEvent, workqueue.RateLimitingInterface) {
}

// Delete implements EventHandler. All delete events are ignored.
func (e *Throttler) Delete(event.DeleteEvent, workqueue.RateLimitingInterface) {
}

// Generic implements EventHandler.
// It pushes at most one event for the same object to the queue during duration.
func (e *Throttler) Generic(evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	if evt.Object == nil {
		return
	}
	r := types.NamespacedName{
		Namespace: evt.Object.GetNamespace(),
		Name:      evt.Object.GetName(),
	}

	e.lock.RLock()
	_, found := e.mapping[r]
	e.lock.RUnlock()
	klog.V(4).Info("received a generic event in the throttler")
	// Skip the event if there is already the same event in the queue
	if found {
		klog.V(4).Info("skip it since there is an event in the throttler")
		return
	}

	// The following code takes the following action:
	// - mark the event is in the queue
	// - push the event to the queue after duration
	// - after duration, mark the event is not in the queue.
	// As a result, there is at most one event pushed to the queue
	// for an object during duration.
	e.lock.Lock()
	e.mapping[r] = struct{}{}
	e.lock.Unlock()

	go func() {
		time.Sleep(e.duration)
		q.Add(reconcile.Request{NamespacedName: r})
		klog.V(4).Infof("add the request to the queue %v", r)
		e.lock.Lock()
		delete(e.mapping, r)
		e.lock.Unlock()
	}()
}
