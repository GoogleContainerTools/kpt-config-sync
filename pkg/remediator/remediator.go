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
	"sync"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/remediator/queue"
	"kpt.dev/configsync/pkg/remediator/reconcile"
	"kpt.dev/configsync/pkg/remediator/watch"
	"kpt.dev/configsync/pkg/status"
	syncerreconcile "kpt.dev/configsync/pkg/syncer/reconcile"
)

// Remediator knows how to keep the state of a Kubernetes cluster in sync with
// a set of declared resources. It processes a work queue of items, and ensures
// each matches the set of declarations passed on instantiation.
//
// The exposed Queue operations are threadsafe - multiple callers may safely
// synchronously add and consume work items.
type Remediator struct {
	watchMgr *watch.Manager
	workers  []*reconcile.Worker
	started  bool
	// The following fields are guarded by the mutex.
	mux sync.Mutex
	// conflictErrs tracks all the management conflicts the remediator encounters,
	// and report to RootSync|RepoSync status.
	conflictErrs []status.ManagementConflictError
}

// Interface is a fake-able subset of the interface Remediator implements that
// accepts a new set of declared configuration.
//
// Placed here to make discovering the production implementation (above) easier.
type Interface interface {
	// NeedsUpdate returns true if the Remediator needs its watches to be updated
	// (typically due to some asynchronous error that occurred).
	NeedsUpdate() bool
	// UpdateWatches starts and stops server-side watches based upon the given map
	// of GVKs which should be watched.
	UpdateWatches(context.Context, map[schema.GroupVersionKind]struct{}) status.MultiError
	// ManagementConflict returns true if one of the watchers noticed a management conflict.
	ManagementConflict() bool
	// ConflictErrors returns the errors the remediator encounters.
	ConflictErrors() []status.ManagementConflictError
}

var _ Interface = &Remediator{}

// New instantiates launches goroutines to make the state of the connected
// cluster match the declared resources.
//
// It is safe for decls to be modified after they have been passed into the
// Remediator.
func New(scope declared.Scope, syncName string, cfg *rest.Config, applier syncerreconcile.Applier, decls *declared.Resources, numWorkers int) (*Remediator, error) {
	q := queue.New(string(scope))
	workers := make([]*reconcile.Worker, numWorkers)
	for i := 0; i < numWorkers; i++ {
		workers[i] = reconcile.NewWorker(scope, syncName, applier, q, decls)
	}

	remediator := &Remediator{
		workers: workers,
	}

	watchMgr, err := watch.NewManager(scope, syncName, cfg, q, decls, nil,
		remediator.addConflictError, remediator.removeConflictError)
	if err != nil {
		return nil, errors.Wrap(err, "creating watch manager")
	}

	remediator.watchMgr = watchMgr
	return remediator, nil
}

// Start begins the asynchronous processes for the Remediator's reconcile workers.
func (r *Remediator) Start(ctx context.Context) {
	if r.started {
		return
	}
	for _, worker := range r.workers {
		go worker.Run(ctx)
	}
	r.started = true
}

// NeedsUpdate implements Interface.
func (r *Remediator) NeedsUpdate() bool {
	return r.watchMgr.NeedsUpdate()
}

// UpdateWatches implements Interface.
func (r *Remediator) UpdateWatches(ctx context.Context, gvks map[schema.GroupVersionKind]struct{}) status.MultiError {
	return r.watchMgr.UpdateWatches(ctx, gvks)
}

// ManagementConflict implements Interface.
func (r *Remediator) ManagementConflict() bool {
	return r.watchMgr.ManagementConflict()
}

// ConflictErrors implements Interface.
func (r *Remediator) ConflictErrors() []status.ManagementConflictError {
	return r.conflictErrs
}

func (r *Remediator) addConflictError(e status.ManagementConflictError) {
	r.mux.Lock()
	defer r.mux.Unlock()

	for _, existingErr := range r.conflictErrs {
		if e.Error() == existingErr.Error() {
			return
		}
	}
	r.conflictErrs = append(r.conflictErrs, e)
}

func (r *Remediator) removeConflictError(e status.ManagementConflictError) {
	r.mux.Lock()
	defer r.mux.Unlock()

	newErrs := make([]status.ManagementConflictError, len(r.conflictErrs))
	i := 0
	for _, existingErr := range r.conflictErrs {
		if e.Error() != existingErr.Error() {
			newErrs[i] = existingErr
			i++
		} else {
			klog.Infof("Conflict error resolved: %v", e)
		}
	}
	r.conflictErrs = newErrs[0:i]
}
