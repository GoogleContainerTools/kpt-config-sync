// Copyright 2024 Google LLC
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

package finalizer

import (
	"context"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/status"
)

type baseFinalizer struct {
	Destroyer applier.Destroyer
}

// destroy calls Destroyer.Destroy, collects errors, and handles logging,
// similar to Updater.apply.
func (bf *baseFinalizer) destroy(ctx context.Context) status.MultiError {
	var err status.MultiError
	eventHandler := func(event applier.Event) {
		if errEvent, ok := event.(applier.ErrorEvent); ok {
			if err == nil {
				err = errEvent.Error
			} else {
				err = status.Append(err, errEvent.Error)
			}
		}
	}
	klog.Info("Destroyer starting...")
	// start := time.Now()
	objStatusMap, syncStats := bf.Destroyer.Destroy(ctx, eventHandler)
	if !syncStats.Empty() {
		klog.Infof("Destroyer made new progress: %s", syncStats.String())
		objStatusMap.Log(klog.V(0))
	}
	// TODO: should we have a destroy duration metric?
	// We don't have the commit here, so we can't send the apply metric.
	// metrics.RecordApplyDuration(ctx, metrics.StatusTagKey(errs), commit, start)
	if err != nil {
		klog.Warningf("Destroyer failed: %v", err)
		return err
	}
	klog.Info("Destroyer succeeded")
	return nil
}
