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

package remediator

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/remediator/conflict"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/remediator/reconcile"
	"kpt.dev/configsync/pkg/remediator/watch"
	"kpt.dev/configsync/pkg/status"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
)

// Remediator knows how to keep the state of a Kubernetes cluster in sync with
// a set of declared resources. It processes a work queue of items, and ensures
// each matches the set of declarations passed on instantiation.
//
// The exposed Queue operations are threadsafe - multiple callers may safely
// synchronously add and consume work items.
type Remediator struct {
	watchMgr *watch.Manager

	// lifecycleMux guards start/stop/add/remove of the workers, as well as
	// updates to queue, parentContext, doneCh, and stopFn.
	lifecycleMux sync.RWMutex
	// workers pull objects from the queue and remediate them
	workers []*reconcile.Worker
	// objectQueue is a queue of objects that have received watch events and
	// need to be processed by the workers.
	objectQueue *queue.ObjectQueue
	// parentContext is set by Start and should be cancelled by the caller when
	// the Remediator should stop.
	parentContext context.Context
	// doneCh indicates that the workers have fully stopped after parentContext
	// is cancelled.
	doneCh chan struct{}
	// stopFn cancels the internal context, stopping the workers.
	stopFn context.CancelFunc
	// running is true if Start has been called and the remediator is not
	// currently paused.
	running bool

	conflictHandler conflict.Handler
	fightHandler    fight.Handler
}

// Interface is a fake-able subset of the interface Remediator implements that
// accepts a new set of declared configuration.
//
// Placed here to make discovering the production implementation (above) easier.
type Interface interface {
	// Remediating returns true if workers are running and watchers are watching.
	Remediating() bool
	// Pause the Remediator by stopping the workers and waiting for them to be done.
	Pause()
	// Resume the Remediator by starting the workers.
	Resume()
	// NeedsUpdate returns true if the Remediator needs its watches to be updated
	// (typically due to some asynchronous error that occurred).
	NeedsUpdate() bool
	// AddWatches starts server-side watches based upon the given map of GVKs
	// which should be watched.
	AddWatches(context.Context, map[schema.GroupVersionKind]struct{}) status.MultiError
	// UpdateWatches starts and stops server-side watches based upon the given map
	// of GVKs which should be watched.
	UpdateWatches(context.Context, map[schema.GroupVersionKind]struct{}) status.MultiError
}

var _ Interface = &Remediator{}

// New instantiates launches goroutines to make the state of the connected
// cluster match the declared resources.
//
// It is safe for decls to be modified after they have been passed into the
// Remediator.
func New(
	scope declared.Scope,
	syncName string,
	cfg *rest.Config,
	applier syncerreconcile.Applier,
	conflictHandler conflict.Handler,
	fightHandler fight.Handler,
	decls *declared.Resources,
	numWorkers int,
) (*Remediator, error) {
	q := queue.New(scope.String())
	workers := make([]*reconcile.Worker, numWorkers)
	for i := 0; i < numWorkers; i++ {
		workers[i] = reconcile.NewWorker(scope, syncName, applier, q, decls, conflictHandler, fightHandler)
	}

	remediator := &Remediator{
		workers:         workers,
		objectQueue:     q,
		fightHandler:    fightHandler,
		conflictHandler: conflictHandler,
	}

	watchMgr, err := watch.NewManager(scope, syncName, cfg, q, decls, nil, conflictHandler)
	if err != nil {
		return nil, fmt.Errorf("creating watch manager: %w", err)
	}

	remediator.watchMgr = watchMgr
	return remediator, nil
}

// Start the Remediator's asynchronous reconcile workers.
// Returns a done channel that will be closed after the context is cancelled and
// all the workers have exited.
func (r *Remediator) Start(ctx context.Context) <-chan struct{} {
	r.lifecycleMux.Lock()
	defer r.lifecycleMux.Unlock()

	if r.parentContext != nil {
		panic("Remediator must only be started once!")
	}

	klog.V(1).Info("Remediator starting...")
	r.parentContext = ctx
	r.startWorkers()
	klog.V(3).Info("Remediator started")

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh) // inform caller the workers are done
		<-ctx.Done()        // wait until cancelled
		klog.V(1).Info("Remediator stopping...")
		r.objectQueue.ShutDown()
		<-r.doneCh // wait until workers are done (if running)
		klog.V(3).Info("Remediator stopped")
	}()
	return doneCh
}

// startWorkers starts the workers and sets doneCh & stopFn.
// This should always be called while lifecycleMux is locked.
func (r *Remediator) startWorkers() {
	if r.running {
		return
	}

	ctx, cancel := context.WithCancel(r.parentContext)

	doneCh := make(chan struct{})
	var wg sync.WaitGroup
	for i := range r.workers {
		worker := r.workers[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker.Run(ctx)
		}()
	}
	go func() {
		defer close(doneCh)
		wg.Wait()
	}()

	r.doneCh = doneCh
	r.stopFn = cancel
	r.running = true
}

// stopWorkers stops the workers and waits for them to exit.
// This should always be called while lifecycleMux is locked.
func (r *Remediator) stopWorkers() {
	if !r.running {
		return
	}

	r.stopFn() // tell the workers to stop
	<-r.doneCh // wait until workers are done (if running)
	r.running = false
}

// Remediating returns true if workers are running and watchers are watching.
func (r *Remediator) Remediating() bool {
	r.lifecycleMux.RLock()
	defer r.lifecycleMux.RUnlock()
	return r.running && r.watchMgr.Watching()
}

// Pause the Remediator by stopping the workers and waiting for them to be done.
func (r *Remediator) Pause() {
	r.lifecycleMux.Lock()
	defer r.lifecycleMux.Unlock()

	klog.V(1).Info("Remediator pausing...")
	r.stopWorkers()
	klog.V(3).Info("Remediator paused")
}

// Resume the Remediator by starting the workers.
func (r *Remediator) Resume() {
	r.lifecycleMux.Lock()
	defer r.lifecycleMux.Unlock()

	klog.V(1).Info("Remediator resuming...")
	r.startWorkers()
	klog.V(3).Info("Remediator resumed")
}

// NeedsUpdate implements Interface.
func (r *Remediator) NeedsUpdate() bool {
	return r.watchMgr.NeedsUpdate()
}

// AddWatches implements Interface.
func (r *Remediator) AddWatches(ctx context.Context, gvks map[schema.GroupVersionKind]struct{}) status.MultiError {
	return r.watchMgr.AddWatches(ctx, gvks)
}

// UpdateWatches implements Interface.
func (r *Remediator) UpdateWatches(ctx context.Context, gvks map[schema.GroupVersionKind]struct{}) status.MultiError {
	return r.watchMgr.UpdateWatches(ctx, gvks)
}
