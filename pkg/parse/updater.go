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
	// SyncErrorCache caches the sync errors from the various reconciler
	// sub-components running in parallel. This allows batching updates and
	// pushing them asynchronously.
	SyncErrorCache *SyncErrorCache

	updateMux sync.RWMutex
}

func (u *Updater) needToUpdateWatch() bool {
	return u.Remediator.NeedsUpdate()
}

func (u *Updater) managementConflict() bool {
	return u.Remediator.ManagementConflict()
}

// Remediating returns true if the Remediator is remediating.
func (u *Updater) Remediating() bool {
	return u.Remediator.Remediating()
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
func (u *Updater) Update(ctx context.Context, cache *cacheForCommit) status.MultiError {
	u.updateMux.Lock()
	defer u.updateMux.Unlock()

	return u.update(ctx, cache)
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
		declaredObjs, _ := u.Resources.DeclaredObjects()
		if err := u.apply(ctx, declaredObjs, cache.source.commit); err != nil {
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
	u.SyncErrorCache.SetValidationErrs(err)
	if err != nil {
		klog.Warningf("Failed to validate declared resources: %v", err)
		return nil, err
	}
	klog.V(3).Info("Declared resources updated...")
	return objs, nil
}

func (u *Updater) apply(ctx context.Context, objs []client.Object, commit string) status.MultiError {
	// Collect errors into a MultiError
	var err status.MultiError
	eventHandler := func(event applier.Event) {
		if errEvent, ok := event.(applier.ErrorEvent); ok {
			if err == nil {
				err = errEvent.Error
			} else {
				err = status.Append(err, errEvent.Error)
			}
			if conflictErr, ok := errEvent.Error.(status.ManagementConflictError); ok {
				u.SyncErrorCache.AddConflictError(conflictErr)
			} else {
				u.SyncErrorCache.AddApplyError(errEvent.Error)
			}
		}
	}
	klog.V(1).Info("Applier starting...")
	start := time.Now()
	u.SyncErrorCache.ResetApplyErrors()
	objStatusMap, syncStats := u.Applier.Apply(ctx, eventHandler, objs)
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
	u.SyncErrorCache.SetWatchErrs(watchErrs)
	if watchErrs != nil {
		klog.Warningf("Failed to update resource watches: %v", watchErrs)
		return watchErrs
	}
	klog.V(3).Info("Remediator watches updated")
	return nil
}
