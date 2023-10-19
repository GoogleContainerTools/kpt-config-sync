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

	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
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

// updater mutates the most-recently-seen versions of objects stored in memory.
type updater struct {
	scope      declared.Scope
	resources  *declared.Resources
	remediator remediator.Interface
	applier    applier.Applier

	errorMux       sync.RWMutex
	validationErrs status.MultiError
	watchErrs      status.MultiError

	updateMux sync.RWMutex
	updating  bool
}

func (u *updater) needToUpdateWatch() bool {
	return u.remediator.NeedsUpdate()
}

func (u *updater) managementConflict() bool {
	return u.remediator.ManagementConflict()
}

// Errors returns the latest known set of errors from the updater.
// This method is safe to call while Update is running.
func (u *updater) Errors() status.MultiError {
	u.errorMux.RLock()
	defer u.errorMux.RUnlock()

	var errs status.MultiError
	errs = status.Append(errs, u.conflictErrors())
	errs = status.Append(errs, u.fightErrors())
	errs = status.Append(errs, u.validationErrs)
	errs = status.Append(errs, u.applier.Errors())
	errs = status.Append(errs, u.watchErrs)
	return errs
}

// conflictErrors converts []ManagementConflictError into []MultiErrors.
// This method is safe to call while Update is running.
func (u *updater) conflictErrors() status.MultiError {
	var errs status.MultiError
	for _, conflictErr := range u.remediator.ConflictErrors() {
		errs = status.Append(errs, conflictErr)
	}
	return errs
}

// fightErrors converts []Error into []MultiErrors.
// This method is safe to call while Update is running.
func (u *updater) fightErrors() status.MultiError {
	var errs status.MultiError
	for _, fightErr := range u.remediator.FightErrors() {
		errs = status.Append(errs, fightErr)
	}
	return errs
}

func (u *updater) setValidationErrs(errs status.MultiError) {
	u.errorMux.Lock()
	defer u.errorMux.Unlock()
	u.validationErrs = errs
}

func (u *updater) setWatchErrs(errs status.MultiError) {
	u.errorMux.Lock()
	defer u.errorMux.Unlock()
	u.watchErrs = errs
}

// Updating returns true if the Update method is running.
func (u *updater) Updating() bool {
	return u.updating
}

// declaredCRDs returns the list of CRDs which are present in the updater's
// declared resources.
func (u *updater) declaredCRDs() ([]*v1beta1.CustomResourceDefinition, status.MultiError) {
	var crds []*v1beta1.CustomResourceDefinition
	declaredObjs, _ := u.resources.DeclaredUnstructureds()
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
func (u *updater) Update(ctx context.Context, cache *CacheForCommit) status.MultiError {
	u.updateMux.Lock()
	u.updating = true
	defer func() {
		u.updating = false
		u.updateMux.Unlock()
	}()

	updateErrs := u.update(ctx, cache)

	// Prepend current conflict and fight errors
	var errs status.MultiError
	errs = status.Append(errs, u.conflictErrors())
	errs = status.Append(errs, u.fightErrors())
	errs = status.Append(errs, updateErrs)
	return errs
}

// update performs most of the work for `Update`, making it easier to
// consistently prepend the conflict errors.
func (u *updater) update(ctx context.Context, cache *CacheForCommit) status.MultiError {
	// Stop remediator workers.
	// This prevents objects been updated in the wrong order (dependencies).
	// Continue watching previously declared objects and updating the queue.
	// Queued objects will be remediated when the workers are started again.
	u.remediator.Pause()

	// block and wait until the current remediation is done.
	u.remediator.WaitRemediating()

	// clear remediate cache to abandon obsolete remediate requests.
	u.remediator.ClearCache()

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
		declaredObjs, _ := u.resources.DeclaredObjects()
		_, err := u.apply(ctx, declaredObjs, cache.source.commit)
		if err != nil {
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
		declaredGVKs, _ := u.resources.DeclaredGVKs()
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
	u.remediator.Resume()

	return nil
}

func (u *updater) declare(ctx context.Context, objs []client.Object, commit string) ([]client.Object, status.MultiError) {
	klog.V(1).Info("Declared resources updating...")
	objs, err := u.resources.Update(ctx, objs, commit)
	u.setValidationErrs(err)
	if err != nil {
		klog.Warningf("Failed to validate declared resources: %v", err)
		return nil, err
	}
	klog.V(3).Info("Declared resources updated")
	return objs, nil
}

func (u *updater) apply(ctx context.Context, objs []client.Object, commit string) (map[schema.GroupVersionKind]struct{}, status.MultiError) {
	klog.V(1).Info("Applier starting...")
	start := time.Now()
	gvks, err := u.applier.Apply(ctx, objs)
	metrics.RecordApplyDuration(ctx, metrics.StatusTagKey(err), commit, start)
	if err != nil {
		klog.Warningf("Failed to apply declared resources: %v", err)
		return nil, err
	}
	klog.V(3).Info("Applier stopped")
	return gvks, nil
}

// watch updates the Remediator's watches to start new ones and stop old
// ones.
func (u *updater) watch(ctx context.Context, gvks map[schema.GroupVersionKind]struct{}) status.MultiError {
	klog.V(1).Info("Remediator watches updating...")
	watchErrs := u.remediator.UpdateWatches(ctx, gvks)
	u.setWatchErrs(watchErrs)
	if watchErrs != nil {
		klog.Warningf("Failed to update resource watches: %v", watchErrs)
		return watchErrs
	}
	klog.V(3).Info("Remediator watches updated")
	return nil
}
