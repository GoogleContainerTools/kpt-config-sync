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
	"math"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/status"
)

const (
	retriesBeforeStartingBackoff = 5
	maxRetryInterval             = time.Duration(5) * time.Minute
)

type sourceStatus struct {
	commit     string
	errs       status.MultiError
	lastUpdate metav1.Time
}

func (gs sourceStatus) equal(other sourceStatus) bool {
	return gs.commit == other.commit && status.DeepEqual(gs.errs, other.errs)
}

type renderingStatus struct {
	commit     string
	message    string
	errs       status.MultiError
	lastUpdate metav1.Time
}

func (rs renderingStatus) equal(other renderingStatus) bool {
	return rs.commit == other.commit && rs.message == other.message && status.DeepEqual(rs.errs, other.errs)
}

type reconcilerState struct {
	// lastApplied keeps the state for the last successful-applied syncDir.
	lastApplied string

	// sourceStatus tracks info from the `Status.Source` field of a RepoSync/RootSync.
	sourceStatus

	// renderingStatus tracks info from the `Status.Rendering` field of a RepoSync/RootSync.
	renderingStatus

	// syncStatus tracks info from the `Status.Sync` field of a RepoSync/RootSync.
	syncStatus sourceStatus

	// syncingConditionLastUpdate tracks when the `Syncing` condition was updated most recently.
	syncingConditionLastUpdate metav1.Time

	// cache tracks the progress made by the reconciler for a source commit.
	cache cacheForCommit
}

func (s *reconcilerState) checkpoint() {
	applied := s.cache.source.syncDir.OSPath()
	if applied == s.lastApplied {
		return
	}
	klog.Infof("Reconciler checkpoint updated to %s", applied)
	s.cache.errs = nil
	s.lastApplied = applied
	s.cache.needToRetry = false
	s.cache.reconciliationWithSameErrs = 0
	s.cache.nextRetryTime = time.Time{}
	s.cache.errs = nil
}

// reset sets the reconciler to retry in the next second because the rendering
// status is not available
func (s *reconcilerState) reset() {
	klog.Infof("Resetting reconciler checkpoint because the rendering status is not available yet")
	s.resetCache()
	s.lastApplied = ""
	s.cache.needToRetry = true
	s.cache.nextRetryTime = time.Now().Add(time.Second)
}

// invalidate logs the errors, clears the state tracking information.
// invalidate does not clean up the `s.cache`.
func (s *reconcilerState) invalidate(errs status.MultiError) {
	klog.Errorf("Invalidating reconciler checkpoint: %v", status.FormatSingleLine(errs))
	oldErrs := s.cache.errs
	s.cache.errs = errs
	// Invalidate state on error since this could be the result of switching
	// branches or some other operation where inverting the operation would
	// result in repeating a previous state that was checkpointed.
	s.lastApplied = ""
	s.cache.needToRetry = true
	if status.DeepEqual(oldErrs, s.cache.errs) {
		s.cache.reconciliationWithSameErrs++
	} else {
		s.cache.reconciliationWithSameErrs = 1
	}
	s.cache.nextRetryTime = calculateNextRetryTime(s.cache.reconciliationWithSameErrs)
}

func calculateNextRetryTime(retries int) time.Time {
	// For the first several retries, the reconciler waits 1 second before retrying.
	if retries <= retriesBeforeStartingBackoff {
		return time.Now().Add(time.Second)
	}

	// For the remaining retries, the reconciler does exponential backoff retry up to 5 minutes.
	// i.e., 1s, 2s, 4s, 8s, 16s, 32s, 64s, 128s, 256s, 5m, 5m, ...
	seconds := int64(math.Pow(2, float64(retries-retriesBeforeStartingBackoff)))
	duration := time.Duration(seconds) * time.Second
	if duration > maxRetryInterval {
		duration = maxRetryInterval
	}
	return time.Now().Add(duration)
}

// resetCache resets the whole cache.
//
// resetCache is called when a new source commit is detected.
func (s *reconcilerState) resetCache() {
	s.cache = cacheForCommit{}
}

// resetAllButGitState resets the whole cache except for the cached sourceState.
//
// resetAllButGitState is called when:
//   * a force-resync happens, or
//   * one of the watchers noticed a management conflict.
func (s *reconcilerState) resetAllButGitState() {
	git := s.cache.source
	s.cache = cacheForCommit{}
	s.cache.source = git
}

// needToSetSourceStatus returns true if `p.setSourceStatus` should be called.
func (s *reconcilerState) needToSetSourceStatus(newStatus sourceStatus) bool {
	return !newStatus.equal(s.sourceStatus) || s.sourceStatus.lastUpdate.IsZero() || s.sourceStatus.lastUpdate.Before(&s.syncingConditionLastUpdate)
}

// needToSetSyncStatus returns true if `p.SetSyncStatus` should be called.
func (s *reconcilerState) needToSetSyncStatus(newStatus sourceStatus) bool {
	return !newStatus.equal(s.syncStatus) || s.syncStatus.lastUpdate.IsZero() || s.syncStatus.lastUpdate.Before(&s.syncingConditionLastUpdate)
}
