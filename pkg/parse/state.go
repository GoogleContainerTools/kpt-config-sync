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
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util"
)

type reconcilerState struct {
	// lastApplied keeps the state for the last successful-applied syncDir.
	lastApplied string

	// status contains fields that map to RSync status fields.
	status *ReconcilerStatus

	// cache tracks the progress made by the reconciler for a source commit.
	cache cacheForCommit

	// backoff defines the duration to wait before retries
	// backoff is initialized to `defaultBackoff()` when a reconcilerState struct is created.
	// backoff is updated before a retry starts.
	// backoff should only be reset back to `defaultBackoff()` when a new commit is detected.
	backoff wait.Backoff

	retryTimer clock.Timer

	retryPeriod time.Duration
}

// retryLimit defines the maximal number of retries allowed on a given commit.
const retryLimit = 12

// The returned backoff includes 12 steps.
// Here is an example of the duration between steps:
//
//	1.055843837s, 2.085359785s, 4.229560375s, 8.324724174s,
//	16.295984061s, 34.325711987s, 1m5.465642392s, 2m18.625713221s,
//	4m24.712222056s, 9m18.97652295s, 17m15.344384599s, 35m15.603237976s.
func defaultBackoff() wait.Backoff {
	return util.BackoffWithDurationAndStepLimit(0, retryLimit)
}

func (s *reconcilerState) checkpoint() {
	applied := s.cache.source.syncDir.OSPath()
	if applied == s.lastApplied {
		return
	}
	klog.Infof("Reconciler checkpoint updated to %s", applied)
	s.lastApplied = applied
	s.cache.needToRetry = false
}

// reset sets the reconciler to retry in the next second because the rendering
// status is not available
func (s *reconcilerState) reset() {
	klog.Infof("Resetting reconciler checkpoint because the rendering status is not available yet")
	s.resetCache()
	s.lastApplied = ""
	s.cache.needToRetry = true
}

// invalidate logs the errors, clears the state tracking information.
// invalidate does not clean up the `s.cache`.
func (s *reconcilerState) invalidate(errs status.MultiError) {
	klog.Errorf("Invalidating reconciler checkpoint: %v", status.FormatSingleLine(errs))
	// Invalidate state on error since this could be the result of switching
	// branches or some other operation where inverting the operation would
	// result in repeating a previous state that was checkpointed.
	s.lastApplied = ""
	s.cache.needToRetry = true
}

// resetCache resets the whole cache.
//
// resetCache is called when a new source commit is detected.
func (s *reconcilerState) resetCache() {
	s.cache = cacheForCommit{}
}

// resetPartialCache resets the whole cache except for the cached sourceState and the cached needToRetry.
// The cached sourceState will not be reset to avoid reading all the source files unnecessarily.
// The cached needToRetry will not be reset to avoid resetting the backoff retries.
//
// resetPartialCache is called when:
//   - a force-resync happens, or
//   - one of the watchers noticed a management conflict.
func (s *reconcilerState) resetPartialCache() {
	source := s.cache.source
	needToRetry := s.cache.needToRetry
	s.cache = cacheForCommit{}
	s.cache.source = source
	s.cache.needToRetry = needToRetry
}
