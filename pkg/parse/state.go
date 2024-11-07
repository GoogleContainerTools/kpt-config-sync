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
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/status"
)

// ReconcilerState is the current state of the Reconciler, including progress
// indicators and in-memory cache for each of the reconciler stages:
// - Fetch
// - Read
// - Render/Hydrate
// - Parse/Validate
// - Update
//
// ReconcilerState also includes a cache of the RSync spec and status
// (ReconcilerStatus).
//
// TODO: break up cacheForCommit into phase-based caches
// TODO: move sourceState into ReconcilerState so the RSync spec and status are next to each other
type ReconcilerState struct {
	// lastApplied keeps the state for the last successful-applied syncDir.
	lastApplied string

	// status contains fields that map to RSync status fields.
	status *ReconcilerStatus

	// cache tracks the progress made by the reconciler for a source commit.
	cache cacheForCommit

	syncErrorCache *SyncErrorCache
}

func (s *ReconcilerState) checkpoint() {
	applied := s.cache.source.syncDir.OSPath()
	if applied == s.lastApplied {
		return
	}
	klog.Infof("Reconciler checkpoint updated to %s", applied)
	s.lastApplied = applied
	s.cache.needToRetry = false
}

// invalidate logs the errors, clears the state tracking information.
// invalidate does not clean up the `s.cache`.
func (s *ReconcilerState) invalidate(errs status.MultiError) {
	if status.AllTransientErrors(errs) {
		klog.Infof("Reconciler checkpoint invalidated: %v", status.FormatSingleLine(errs))
	} else {
		klog.Errorf("Reconciler checkpoint invalidated: %v", status.FormatSingleLine(errs))
	}
	// Invalidate state on error since this could be the result of switching
	// branches or some other operation where inverting the operation would
	// result in repeating a previous state that was checkpointed.
	s.lastApplied = ""
	s.cache.needToRetry = true
}

// resetCache resets the whole cache.
//
// resetCache is called when a new source commit is detected.
func (s *ReconcilerState) resetCache() {
	s.cache = cacheForCommit{}
}

// resetPartialCache resets the whole cache except for the cached sourceState and the cached needToRetry.
// The cached sourceState will not be reset to avoid reading all the source files unnecessarily.
// The cached needToRetry will not be reset to avoid resetting the backoff retries.
//
// resetPartialCache is called when:
//   - a force-resync happens, or
//   - one of the watchers noticed a management conflict.
func (s *ReconcilerState) resetPartialCache() {
	source := s.cache.source
	needToRetry := s.cache.needToRetry
	s.cache = cacheForCommit{}
	s.cache.source = source
	s.cache.needToRetry = needToRetry
}

// SyncErrors returns all the sync errors, including remediator errors,
// validation errors, applier errors, and watch update errors.
func (s *ReconcilerState) SyncErrors() status.MultiError {
	return s.syncErrorCache.Errors()
}
