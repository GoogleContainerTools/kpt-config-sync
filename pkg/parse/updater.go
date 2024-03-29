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

package parse

import (
	"context"
	"sync"
	"time"

	orderedmap "github.com/wk8/go-ordered-map"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/remediator"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/clusterconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Updater mutates the most-recently-seen versions of objects stored in memory.
type Updater struct {
	// Scope defines the scope of the reconciler, either root or namespaced.
	Scope declared.Scope
	// Resources is a set of resources declared in the source of truth.
	*declared.Resources
	// Remediator is the interface Remediator implements that accepts a new set of
	// declared configuration.
	Remediator remediator.Interface
	// Applier is a bulk client for applying a set of desired resource objects and
	// tracking them in a ResourceGroup inventory.
	Applier applier.Applier
	// StatusUpdatePeriod is how often to update the sync status of the RSync
	// while Update is running.
	StatusUpdatePeriod time.Duration
	// SyncStatusUpdater is used to update the RSync sync status while Update is
	// running.
	SyncStatusUpdater SyncStatusUpdater

	// statusMux prevents race conditions reading and writing status fields.
	statusMux            sync.RWMutex
	validationErrs       status.MultiError
	applyErrs            status.MultiError
	watchErrs            status.MultiError
	inventoryObjectCount int
	updating             bool
	commit               string

	// updateMux prevents the Update method from executing in parallel.
	updateMux   sync.RWMutex
	initialized bool
}

func (u *Updater) needToUpdateWatch() bool {
	return u.Remediator.NeedsUpdate()
}

func (u *Updater) managementConflict() bool {
	return u.Remediator.ManagementConflict()
}

// Errors returns the latest known set of errors from the updater.
// This method is safe to call while Update is running.
func (u *Updater) Errors() status.MultiError {
	u.statusMux.RLock()
	defer u.statusMux.RUnlock()

	// Ordering here is important. It needs to be the same as Updater.Update.
	var updateErrs status.MultiError
	updateErrs = status.Append(updateErrs, u.validationErrs)
	updateErrs = status.Append(updateErrs, u.applyErrs)
	updateErrs = status.Append(updateErrs, u.watchErrs)
	return u.prependRemediatorErrors(updateErrs)
}

// prependRemediatorErrors combines remediator and updater errors.
func (u *Updater) prependRemediatorErrors(updateErrs status.MultiError) status.MultiError {
	updaterConflictErrors, remainingUpdateErrs := extractConflictErrors(updateErrs)
	conflictErrs := dedupeConflictErrors(updaterConflictErrors, u.Remediator.ConflictErrors())

	var fightErrs status.MultiError
	for _, fightErr := range u.Remediator.FightErrors() {
		fightErrs = status.Append(fightErrs, fightErr)
	}

	var errs status.MultiError
	errs = status.Append(errs, conflictErrs)
	errs = status.Append(errs, fightErrs)
	errs = status.Append(errs, remainingUpdateErrs)
	return errs
}

func extractConflictErrors(updateErrs status.MultiError) ([]status.ManagementConflictError, status.MultiError) {
	var conflictErrs []status.ManagementConflictError
	var remainingErrs status.MultiError
	if updateErrs != nil {
		for _, updateErr := range updateErrs.Errors() {
			if conflictErr, ok := updateErr.(status.ManagementConflictError); ok {
				conflictErrs = append(conflictErrs, conflictErr)
			} else {
				remainingErrs = status.Append(remainingErrs, updateErr)
			}
		}
	}
	return conflictErrs, remainingErrs
}

// dedupeConflictErrors de-dupes conflict errors from the remediator and updater.
// Remediator conflict errors take precedence, because they have more detail.
func dedupeConflictErrors(updaterConflictErrors, remediatorConflictErrors []status.ManagementConflictError) status.MultiError {
	conflictErrMap := orderedmap.New()
	for _, conflictErr := range updaterConflictErrors {
		conflictErrMap.Set(conflictErr.ConflictingObjectID(), conflictErr)
	}
	for _, conflictErr := range remediatorConflictErrors {
		conflictErrMap.Set(conflictErr.ConflictingObjectID(), conflictErr)
	}
	var conflictErrs status.MultiError
	for pair := conflictErrMap.Oldest(); pair != nil; pair = pair.Next() {
		conflictErrs = status.Append(conflictErrs, pair.Value.(status.ManagementConflictError))
	}
	return conflictErrs
}

func (u *Updater) setValidationErrs(errs status.MultiError) {
	u.statusMux.Lock()
	defer u.statusMux.Unlock()
	u.validationErrs = errs
}

func (u *Updater) applyErrors() status.MultiError {
	u.statusMux.RLock()
	defer u.statusMux.RUnlock()
	return u.applyErrs
}

func (u *Updater) addApplyError(err status.Error) {
	u.statusMux.Lock()
	defer u.statusMux.Unlock()
	u.applyErrs = status.Append(u.applyErrs, err)
}

func (u *Updater) resetApplyErrors() {
	u.statusMux.Lock()
	defer u.statusMux.Unlock()
	u.applyErrs = nil
}

func (u *Updater) setWatchErrs(errs status.MultiError) {
	u.statusMux.Lock()
	defer u.statusMux.Unlock()
	u.watchErrs = errs
}

func (u *Updater) InventoryObjectCount() int {
	u.statusMux.RLock()
	defer u.statusMux.RUnlock()
	return u.inventoryObjectCount
}

func (u *Updater) setInventoryObjectCount(count int) {
	u.statusMux.Lock()
	defer u.statusMux.Unlock()
	u.inventoryObjectCount = count
}

// Updating returns true if the Update method is running.
func (u *Updater) Updating() bool {
	u.statusMux.RLock()
	defer u.statusMux.RUnlock()
	return u.updating
}

func (u *Updater) setUpdating(updating bool) {
	u.statusMux.Lock()
	defer u.statusMux.Unlock()
	u.updating = updating
}

// Commit returns the commit last synced (if not syncing) or being synced (if syncing)
func (u *Updater) Commit() string {
	u.statusMux.RLock()
	defer u.statusMux.RUnlock()
	return u.commit
}

func (u *Updater) setCommit(commit string) {
	u.statusMux.Lock()
	defer u.statusMux.Unlock()
	u.commit = commit
}

// declaredCRDs returns the list of CRDs which are present in the updater's
// declared resources.
func (u *Updater) declaredCRDs() ([]*v1beta1.CustomResourceDefinition, status.MultiError) {
	var crds []*v1beta1.CustomResourceDefinition
	declaredObjs, _ := u.Resources.DeclaredUnstructureds()
	for _, obj := range declaredObjs {
		if obj.GroupVersionKind().GroupKind() != kinds.CustomResourceDefinition() {
			continue
		}
		crd, err := clusterconfig.AsCRD(obj)
		if err != nil {
			return nil, err
		}
		crds = append(crds, crd)
	}
	return crds, nil
}

// Update does the following:
// 1. Pauses the remediator
// 2. Validates and sterilizes the objects
// 3. Updates the declared resource objects in memory
// 4. Applies the objects
// 5. Updates the remediator watches
// 6. Restarts the remediator
//
// Any errors returned will be prepended with any known conflict errors from the
// remediator. This is required to preserve errors that have been reported by
// another reconciler.
func (u *Updater) Update(ctx context.Context, state *reconcilerState) status.MultiError {
	u.updateMux.Lock()
	u.setUpdating(true)
	defer func() {
		u.setUpdating(false)
		u.updateMux.Unlock()
	}()

	// When the reconciler restarts/reschedules, the Updater state is reset.
	// So restore the Updater state from the RSync status.
	if !u.initialized {
		syncStatus, err := u.SyncStatusUpdater.SyncStatus(ctx)
		if err != nil {
			updateErrs := status.Append(nil, err)
			return u.prependRemediatorErrors(updateErrs)
		}
		u.setCommit(syncStatus.commit)
		u.setInventoryObjectCount(syncStatus.objectCount)
		// TODO: Figure out how to categorizes errors into the different kinds of errors
		// For now, the errors aren't even being parsed, so we can skip categorizing them.
		// This means the errors will always be reset when a new sync starts for
		// the first time after the reconciler was restart/rescheduled.
		u.initialized = true
	}

	// Sync the latest commit from source
	if u.Commit() != state.cache.source.commit {
		u.setCommit(state.cache.source.commit)
	}

	klog.V(3).Info("Updating sync status (before sync)")
	if err := u.SetSyncStatus(ctx, state); err != nil {
		updateErrs := status.Append(nil, err)
		return u.prependRemediatorErrors(updateErrs)
	}

	// Start periodic sync status updates.
	// This allows reporting errors received from applier events while applying.
	ctxForUpdateSyncStatus, cancel := context.WithCancel(ctx)
	doneCh := u.startPeriodicSyncStatusUpdates(ctxForUpdateSyncStatus, state)

	klog.V(3).Info("Updater starting...")
	updateErrs := u.update(ctx, &state.cache)
	updateErrs = u.prependRemediatorErrors(updateErrs)
	klog.V(3).Info("Updater stopped")

	// Stop periodic sync status updates and wait for completion, to avoid update conflicts.
	cancel()
	<-doneCh

	u.setUpdating(false)

	klog.V(3).Info("Updating sync status (after sync)")
	if err := u.SetSyncStatus(ctx, state); err != nil {
		updateErrs = status.Append(updateErrs, err)
	}

	return updateErrs
}

// SetSyncStatus updates `.status.sync` and the Syncing condition, if needed,
// as well as `state.syncStatus` and `state.syncingConditionLastUpdate` if
// the update is successful.
func (u *Updater) SetSyncStatus(ctx context.Context, state *reconcilerState) error {
	// Update the RSync status, if necessary
	newSyncStatus := syncStatus{
		syncing:     u.Updating(),
		commit:      u.Commit(),
		objectCount: u.InventoryObjectCount(),
		errs:        u.Errors(),
		lastUpdate:  metav1.Now(),
	}
	if state.needToSetSyncStatus(newSyncStatus) {
		if err := u.SyncStatusUpdater.SetSyncStatus(ctx, newSyncStatus); err != nil {
			return err
		}
		state.syncStatus = newSyncStatus
		state.syncingConditionLastUpdate = newSyncStatus.lastUpdate
	}
	return nil
}

// startPeriodicSyncStatusUpdates updates the sync status periodically until the
// cancellation function of the context is called.
// The returned done channel will be closed after the background goroutine has
// stopped running.
func (u *Updater) startPeriodicSyncStatusUpdates(ctx context.Context, state *reconcilerState) chan struct{} {
	klog.V(3).Info("Periodic sync status updates starting...")
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		updatePeriod := u.StatusUpdatePeriod
		updateTimer := time.NewTimer(updatePeriod)
		defer updateTimer.Stop()
		for {
			select {
			case <-ctx.Done():
				// ctx.Done() is closed when the cancellation function of the context is called.
				klog.V(3).Info("Periodic sync status updates stopped")
				return

			case <-updateTimer.C:
				klog.V(3).Info("Updating sync status (periodic while syncing)")
				if err := u.SetSyncStatus(ctx, state); err != nil {
					klog.Warningf("failed to update sync status: %v", err)
				}
				// Schedule next status update
				updateTimer.Reset(updatePeriod)
			}
		}
	}()
	return doneCh
}

// update performs most of the work for `Update`, making it easier to
// consistently prepend the conflict errors.
func (u *Updater) update(ctx context.Context, cache *cacheForCommit) status.MultiError {
	// Stop remediator workers.
	// This prevents objects been updated in the wrong order (dependencies).
	// Continue watching previously declared objects and updating the queue.
	// Queued objects will be remediated when the workers are started again.
	u.Remediator.Pause()

	// Update the declared resources (source of truth for the Remediator).
	// After this, any objects removed from the declared resources will no
	// longer be remediated, if they drift.
	if !cache.declaredResourcesUpdated {
		objs := filesystem.AsCoreObjects(cache.objsToApply)
		_, err := u.declare(ctx, objs, cache.source.commit)
		if err != nil {
			return err
		}
		// Only mark the declared resources as updated if there were no (non-blocking) parse errors.
		// This ensures the update will be retried until parsing fully succeeds.
		if cache.parserErrs == nil {
			cache.declaredResourcesUpdated = true
		}
	}

	// Apply the declared resources
	if !cache.applied {
		// Update InventoryObjectCount after each inventory update
		superEventHandler := func(event applier.SuperEvent) {
			if inventoryEvent, ok := event.(applier.SuperInventoryEvent); ok {
				objectCount := 0
				inv := inventoryEvent.Inventory
				if inv != nil {
					objectCount = len(inv.Spec.Resources)
				}
				u.setInventoryObjectCount(objectCount)
			}
		}
		declaredObjs, _ := u.Resources.DeclaredObjects()
		if err := u.apply(ctx, superEventHandler, declaredObjs, cache.source.commit); err != nil {
			return err
		}
		// Only mark the commit as applied if there were no (non-blocking) parse errors.
		// This ensures the apply will be retried until parsing fully succeeds.
		if cache.parserErrs == nil {
			cache.applied = true
		}
	}

	// Update the resource watches (triggers for the Remediator).
	if !cache.watchesUpdated {
		declaredGVKs, _ := u.Resources.DeclaredGVKs()
		err := u.watch(ctx, declaredGVKs)
		if err != nil {
			return err
		}
		// Only mark the watches as updated if there were no (non-blocking) parse errors.
		// This ensures the update will be retried until parsing fully succeeds.
		if cache.parserErrs == nil {
			cache.watchesUpdated = true
		}
	}

	// Restart remediator workers.
	// Queue will probably include all the declared objects, but they should
	// show no diff, unless they've been updated asynchronously.
	// Only resume after validation & apply & watch update are successful,
	// otherwise the objects may be updated in the wrong order (dependencies).
	u.Remediator.Resume()

	return nil
}

func (u *Updater) declare(ctx context.Context, objs []client.Object, commit string) ([]client.Object, status.MultiError) {
	klog.V(1).Info("Declared resources updating...")
	objs, err := u.Resources.Update(ctx, objs, commit)
	u.setValidationErrs(err)
	if err != nil {
		klog.Warningf("Failed to validate declared resources: %v", err)
		return nil, err
	}
	klog.V(3).Info("Declared resources updated...")
	return objs, nil
}

func (u *Updater) apply(ctx context.Context, superEventHandler func(applier.SuperEvent), objs []client.Object, commit string) status.MultiError {
	// Wrap the event handler to collect errors
	var err status.MultiError
	wrapper := func(event applier.SuperEvent) {
		if errEvent, ok := event.(applier.SuperErrorEvent); ok {
			if err == nil {
				err = errEvent.Error
			} else {
				err = status.Append(err, errEvent.Error)
			}
			u.addApplyError(errEvent.Error)
		}
		superEventHandler(event)
	}
	klog.V(1).Info("Applier starting...")
	start := time.Now()
	u.resetApplyErrors()
	objStatusMap, syncStats := u.Applier.Apply(ctx, wrapper, objs)
	if syncStats.Empty() {
		klog.V(4).Info("Applier made no new progress")
	} else {
		klog.Infof("Applier made new progress: %s", syncStats.String())
		objStatusMap.Log(klog.V(0))
	}
	metrics.RecordApplyDuration(ctx, metrics.StatusTagKey(err), commit, start)
	if err != nil {
		klog.Warningf("Failed to apply declared resources: %v", err)
		return err
	}
	klog.V(4).Info("Apply completed without error: all resources are up to date.")
	klog.V(3).Info("Applier stopped")
	return nil
}

// watch updates the Remediator's watches to start new ones and stop old
// ones.
func (u *Updater) watch(ctx context.Context, gvks map[schema.GroupVersionKind]struct{}) status.MultiError {
	klog.V(1).Info("Remediator watches updating...")
	watchErrs := u.Remediator.UpdateWatches(ctx, gvks)
	u.setWatchErrs(watchErrs)
	if watchErrs != nil {
		klog.Warningf("Failed to update resource watches: %v", watchErrs)
		return watchErrs
	}
	klog.V(3).Info("Remediator watches updated")
	return nil
}
