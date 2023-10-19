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
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/remediator/cache"
	"kpt.dev/configsync/pkg/remediator/conflict"
	"kpt.dev/configsync/pkg/remediator/watch"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
)

// Remediator knows how to keep the state of a Kubernetes cluster in sync with
// a set of declared resources. It processes a queue of items, and ensures
// each matches the set of declarations passed on instantiation.
//
// The exposed cache is threadsafe - multiple callers may safely
// synchronously add and consume work items.
type Remediator struct {
	watchMgr *watch.Manager
	// remResources is the cache of drifted resources to be corrected by remediator.
	remResources *cache.RemediateResources

	// lifecycleMux guards live status of the Remediator.
	lifecycleMux sync.Mutex
	// canRemediate indicates whether the Remediator can start processing requests.
	canRemediate bool
	// wg is the WaitGroup for the Remediator.
	wg sync.WaitGroup

	conflictHandler conflict.Handler
	fightHandler    fight.Handler
}

// Interface is a fake-able subset of the interface Remediator implements that
// accepts a new set of declared configuration.
//
// Placed here to make discovering the production implementation (above) easier.
type Interface interface {
	// Pause the Remediator by stopping the workers and waiting for them to be done.
	Pause()
	// Resume the Remediator by starting the workers.
	Resume()
	// CanRemediate returns whether the Remediator can start remediating.
	CanRemediate() bool
	// StartRemediating indicates the Remediator starts processing the remediate requests if it can remediate.
	StartRemediating() bool
	// DoneRemediating marks the Remediator as done when it finishes remediation.
	DoneRemediating()
	// WaitRemediating blocks until the Remediator finishes remediating the requests.
	WaitRemediating()
	// ClearCache resets the remediate cache to empty.
	ClearCache()
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
	// FightErrors returns the fight errors (KNV2005) the remediator encounters.
	FightErrors() []status.Error
	AddFightError(core.ID, status.Error)
	RemoveFightError(core.ID)
}

var _ Interface = &Remediator{}

// New instantiates launches goroutines to make the state of the connected
// cluster match the declared resources.
//
// It is safe for decls to be modified after they have been passed into the
// Remediator.
func New(scope declared.Scope, syncName string, cfg *rest.Config, decls *declared.Resources, remResources *cache.RemediateResources) (*Remediator, error) {
	fightHandler := fight.NewHandler()
	conflictHandler := conflict.NewHandler()
	watchMgr, err := watch.NewManager(scope, syncName, cfg, decls, nil, remResources, conflictHandler)
	if err != nil {
		return nil, errors.Wrap(err, "creating watch manager")
	}
	return &Remediator{
		watchMgr:        watchMgr,
		remResources:    remResources,
		fightHandler:    fightHandler,
		conflictHandler: conflictHandler,
	}, nil

}

// Pause the Remediator by stopping the workers and waiting for them to be done.
func (r *Remediator) Pause() {
	r.lifecycleMux.Lock()
	defer r.lifecycleMux.Unlock()

	klog.V(1).Info("Remediator pausing processing new request...")
	r.canRemediate = false
	klog.V(3).Info("Remediator paused processing new request")
}

// Resume the Remediator by starting the workers.
func (r *Remediator) Resume() {
	r.lifecycleMux.Lock()
	defer r.lifecycleMux.Unlock()

	klog.V(1).Info("Remediator resuming processing new request...")
	r.canRemediate = true
	klog.V(3).Info("Remediator resumed processing new request")
}

// CanRemediate returns whether the Remediator can start remediating.
func (r *Remediator) CanRemediate() bool {
	r.lifecycleMux.Lock()
	defer r.lifecycleMux.Unlock()

	return r.canRemediate
}

// StartRemediating marks the Remediator is working to block the Applier if it can
// remediate.
func (r *Remediator) StartRemediating() bool {
	r.lifecycleMux.Lock()
	defer r.lifecycleMux.Unlock()

	if r.canRemediate {
		klog.V(1).Infof("Start remediating...")
		r.wg.Add(1)
	}
	return r.canRemediate
}

// WaitRemediating blocks until the Remediator finishes remediating the requests.
func (r *Remediator) WaitRemediating() {
	klog.V(1).Info("Waiting for the Remediator to complete...")
	r.wg.Wait()
}

// DoneRemediating marks the Remediator as done when it finishes remediation.
func (r *Remediator) DoneRemediating() {
	klog.V(1).Info("Remediator completes...")
	r.wg.Done()
}

// ClearCache resets the remediate cache to empty.
func (r *Remediator) ClearCache() {
	klog.V(1).Info("Clearing cache of the remediate resources...")
	r.remResources.ClearAll()
	klog.V(1).Info("Remediate cache cleared")
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
	return r.conflictHandler.ConflictErrors()
}

// FightErrors implements Interface.
func (r *Remediator) FightErrors() []status.Error {
	return r.fightHandler.FightErrors()
}

// AddFightError implements Interface.
func (r *Remediator) AddFightError(id core.ID, err status.Error) {
	r.fightHandler.AddFightError(id, err)
}

// RemoveFightError implements Interface.
func (r *Remediator) RemoveFightError(id core.ID) {
	r.fightHandler.RemoveFightError(id)
}
