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

package fake

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/remediator"
	"kpt.dev/configsync/pkg/status"
)

// Remediator fakes remediator.Remediator.
//
// This is not in kpt.dev/configsync/pkg/testing/fake because that would cause
// a import loop (remediator -> fake -> remediator).
type Remediator struct {
	ManagementConflictOutput bool
	Watches                  map[schema.GroupVersionKind]struct{}
	AddWatchesError          status.MultiError
	UpdateWatchesError       status.MultiError
	Watching                 bool
	Paused                   bool

	needsUpdate bool
}

// ManagementConflict fakes remediator.Remediator.ManagementConflict
func (r *Remediator) ManagementConflict() bool {
	return r.ManagementConflictOutput
}

// NeedsUpdate fakes remediator.Remediator.NeedsUpdate
func (r *Remediator) NeedsUpdate() bool {
	return r.needsUpdate
}

// Remediating returns true if not paused
func (r *Remediator) Remediating() bool {
	return r.Watching && !r.Paused
}

// Pause fakes remediator.Remediator.Pause
func (r *Remediator) Pause() {
	r.Paused = true
}

// Resume fakes remediator.Remediator.Resume
func (r *Remediator) Resume() {
	r.Paused = false
}

// AddWatches fakes remediator.Remediator.AddWatches
func (r *Remediator) AddWatches(_ context.Context, watches map[schema.GroupVersionKind]struct{}, _ string) status.MultiError {
	r.Watching = true
	if r.Watches == nil {
		r.Watches = watches
	} else {
		for gvk := range watches {
			r.Watches[gvk] = struct{}{}
		}
	}
	return r.AddWatchesError
}

// UpdateWatches fakes remediator.Remediator.UpdateWatches
func (r *Remediator) UpdateWatches(_ context.Context, watches map[schema.GroupVersionKind]struct{}, _ string) status.MultiError {
	r.Watching = true
	r.Watches = watches
	r.needsUpdate = false
	return r.UpdateWatchesError
}

var _ remediator.Interface = &Remediator{}
