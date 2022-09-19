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
	applier    applier.Interface
}

func (u *updater) needToUpdateWatch() bool {
	return u.remediator.NeedsUpdate()
}

func (u *updater) managementConflict() bool {
	return u.remediator.ManagementConflict()
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

// update updates the declared resources in memory, applies the resources, and sets
// up the watches.
func (u *updater) update(ctx context.Context, cache *cacheForCommit) status.MultiError {
	var errs status.MultiError
	objs := filesystem.AsCoreObjects(cache.objsToApply)

	// Update the declared resources so that the Remediator immediately
	// starts enforcing the updated state.
	if !cache.resourceDeclSetUpdated {
		var err status.MultiError
		objs, err = u.resources.Update(ctx, objs)
		if err != nil {
			klog.Infof("Terminate the reconciliation (failed to update the declared resources): %v", err)
			return err
		}

		if cache.parserErrs == nil {
			cache.resourceDeclSetUpdated = true
		}
	}

	var gvks map[schema.GroupVersionKind]struct{}
	if cache.hasApplierResult {
		gvks = cache.applierResult
	} else {
		var applyErrs status.MultiError
		applyStart := time.Now()
		gvks, applyErrs = u.applier.Apply(ctx, objs)
		metrics.RecordApplyDuration(ctx, metrics.StatusTagKey(applyErrs), cache.source.commit, applyStart)
		if applyErrs == nil && cache.parserErrs == nil {
			cache.setApplierResult(gvks)
		}
		errs = status.Append(errs, applyErrs)
	}

	// Update the Remediator's watches to start new ones and stop old ones.
	watchErrs := u.remediator.UpdateWatches(ctx, gvks)
	errs = status.Append(errs, watchErrs)

	return errs
}
