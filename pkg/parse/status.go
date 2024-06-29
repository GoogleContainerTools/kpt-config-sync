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

package parse

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/status"
)

// ReconcilerStatus represents the status of the reconciler.
type ReconcilerStatus struct {
	// SourceStatus tracks info from the `Status.Source` field of a RepoSync/RootSync.
	SourceStatus *SourceStatus

	// RenderingStatus tracks info from the `Status.Rendering` field of a RepoSync/RootSync.
	RenderingStatus *RenderingStatus

	// SyncStatus tracks info from the `Status.Sync` field of a RepoSync/RootSync.
	SyncStatus *SyncStatus

	// SyncingConditionLastUpdate tracks when the `Syncing` condition was updated most recently.
	SyncingConditionLastUpdate metav1.Time
}

// DeepCopy returns a deep copy of the receiver.
// Warning: Go errors are not copy-able. So this isn't a true deep-copy.
func (s *ReconcilerStatus) DeepCopy() *ReconcilerStatus {
	return &ReconcilerStatus{
		SourceStatus:               s.SourceStatus.DeepCopy(),
		RenderingStatus:            s.RenderingStatus.DeepCopy(),
		SyncStatus:                 s.SyncStatus.DeepCopy(),
		SyncingConditionLastUpdate: *s.SyncingConditionLastUpdate.DeepCopy(),
	}
}

// needToSetSourceStatus returns true if `p.setSourceStatus` should be called.
func (s *ReconcilerStatus) needToSetSourceStatus(newStatus *SourceStatus) bool {
	if s.SourceStatus == nil {
		return newStatus != nil
	}
	// Update if not initialized
	if s.SourceStatus.LastUpdate.IsZero() {
		return true
	}
	// Update if source status was last updated before the rendering status
	if s.RenderingStatus != nil && s.SourceStatus.LastUpdate.Before(&s.RenderingStatus.LastUpdate) {
		return true
	}
	// Update if there's a diff
	return !s.SourceStatus.Equals(newStatus)
}

// needToSetSyncStatus returns true if `p.SetSyncStatus` should be called.
func (s *ReconcilerStatus) needToSetSyncStatus(newStatus *SyncStatus) bool {
	if s.SyncStatus == nil {
		return newStatus != nil
	}
	// Update if not initialized
	if s.SyncStatus.LastUpdate.IsZero() {
		return true
	}
	// Update if sync status was last updated before the rendering status
	if s.RenderingStatus != nil && s.SyncStatus.LastUpdate.Before(&s.RenderingStatus.LastUpdate) {
		return true
	}
	// Update if sync status was last updated before the source status
	if s.SourceStatus != nil && s.SyncStatus.LastUpdate.Before(&s.SourceStatus.LastUpdate) {
		return true
	}
	// Update if there's a diff
	return !s.SyncStatus.Equals(newStatus)
}

// SourceStatus represents the status of the source stage of the pipeline.
type SourceStatus struct {
	Commit     string
	Errs       status.MultiError
	LastUpdate metav1.Time
}

// DeepCopy returns a deep copy of the receiver.
// Warning: Go errors are not copy-able. So this isn't a true deep-copy.
func (gs *SourceStatus) DeepCopy() *SourceStatus {
	if gs == nil {
		return nil
	}
	return &SourceStatus{
		Commit:     gs.Commit,
		Errs:       gs.Errs,
		LastUpdate: *gs.LastUpdate.DeepCopy(),
	}
}

// Equals returns true if the specified SourceStatus equals this
// SourceStatus, excluding the LastUpdate timestamp.
func (gs *SourceStatus) Equals(other *SourceStatus) bool {
	if gs == nil {
		return other == nil
	}
	return gs.Commit == other.Commit &&
		status.DeepEqual(gs.Errs, other.Errs)
}

// RenderingStatus represents the status of the rendering stage of the pipeline.
type RenderingStatus struct {
	Commit     string
	Message    string
	Errs       status.MultiError
	LastUpdate metav1.Time
	// RequiresRendering indicates whether the sync source has dry configs
	// only used internally (not surfaced on RSync status)
	RequiresRendering bool
}

// DeepCopy returns a deep copy of the receiver.
// Warning: Go errors are not copy-able. So this isn't a true deep-copy.
func (rs *RenderingStatus) DeepCopy() *RenderingStatus {
	if rs == nil {
		return nil
	}
	return &RenderingStatus{
		Commit:            rs.Commit,
		Message:           rs.Message,
		Errs:              rs.Errs,
		LastUpdate:        *rs.LastUpdate.DeepCopy(),
		RequiresRendering: rs.RequiresRendering,
	}
}

// Equals returns true if the specified RenderingStatus equals this
// RenderingStatus, excluding the LastUpdate timestamp.
func (rs *RenderingStatus) Equals(other *RenderingStatus) bool {
	if rs == nil {
		return other == nil
	}
	return rs.Commit == other.Commit &&
		rs.Message == other.Message &&
		status.DeepEqual(rs.Errs, other.Errs)
}

// SyncStatus represents the status of the sync stage of the pipeline.
type SyncStatus struct {
	Syncing    bool
	Commit     string
	Errs       status.MultiError
	LastUpdate metav1.Time
}

// DeepCopy returns a deep copy of the receiver.
// Warning: Go errors are not copy-able. So this isn't a true deep-copy.
func (ss *SyncStatus) DeepCopy() *SyncStatus {
	if ss == nil {
		return nil
	}
	return &SyncStatus{
		Syncing:    ss.Syncing,
		Commit:     ss.Commit,
		Errs:       ss.Errs,
		LastUpdate: *ss.LastUpdate.DeepCopy(),
	}
}

// Equals returns true if the specified SyncStatus equals this
// SyncStatus, excluding the LastUpdate timestamp.
func (ss *SyncStatus) Equals(other *SyncStatus) bool {
	if ss == nil {
		return other == nil
	}
	return ss.Syncing == other.Syncing &&
		ss.Commit == other.Commit &&
		status.DeepEqual(ss.Errs, other.Errs)
}
