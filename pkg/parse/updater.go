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
	for _, conflictErr := range u.remediator.ConflictErrors() {
		errs = status.Append(errs, conflictErr)
	}
	errs = status.Append(errs, u.validationErrs)
	errs = status.Append(errs, u.applier.Errors())
	errs = status.Append(errs, u.watchErrs)
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
	for _, obj := range u.resources.Declarations() {
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

// Update updates the declared resources in memory, applies the resources, and sets
// up the watches.
func (u *updater) Update(ctx context.Context, cache *cacheForCommit) status.MultiError {
	u.updateMux.Lock()
	u.updating = true
	defer func() {
		u.updating = false
		u.updateMux.Unlock()
	}()

	objs := filesystem.AsCoreObjects(cache.objsToApply)

	// Update the declared resources so that the Remediator immediately
	// starts enforcing the updated state.
	if !cache.resourceDeclSetUpdated {
		var validationErrs status.MultiError
		objs, validationErrs = u.resources.Update(ctx, objs)
		u.setValidationErrs(validationErrs)
		if validationErrs != nil {
			klog.Warningf("Failed to validate declared resources: %v", validationErrs)
			return validationErrs
		}

		if cache.parserErrs == nil {
			cache.resourceDeclSetUpdated = true
		}
	}
	// else - there were no previous validation errors

	var gvks map[schema.GroupVersionKind]struct{}
	var applyErrs status.MultiError
	if cache.hasApplierResult {
		gvks = cache.applierResult
		// there were no previous applier errors
	} else {
		applyStart := time.Now()
		gvks, applyErrs = u.applier.Apply(ctx, objs)
		metrics.RecordApplyDuration(ctx, metrics.StatusTagKey(applyErrs), cache.source.commit, applyStart)
		if applyErrs != nil {
			klog.Warningf("Failed to apply declared resources: %v", applyErrs)
		} else if cache.parserErrs == nil {
			cache.setApplierResult(gvks)
		}
	}

	// Update the Remediator's watches to start new ones and stop old ones.
	watchErrs := u.remediator.UpdateWatches(ctx, gvks)
	u.setWatchErrs(watchErrs)
	if watchErrs != nil {
		klog.Warningf("Failed to update resource watches: %v", watchErrs)
	}

	// Prepend any conflict errors that happened during updating
	var errs status.MultiError
	for _, conflictErr := range u.remediator.ConflictErrors() {
		errs = status.Append(errs, conflictErr)
	}
	errs = status.Append(errs, applyErrs)
	errs = status.Append(errs, watchErrs)
	return errs
}
