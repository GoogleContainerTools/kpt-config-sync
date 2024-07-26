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
	"sync"

	"kpt.dev/configsync/pkg/remediator/conflict"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/reconcile/fight"
)

// SyncErrorCache is a collection of sync errors, locked for thread-safe use.
type SyncErrorCache struct {
	// Errors from the Remediator & Updater
	conflictHandler conflict.Handler
	// Errors from the Remediator
	fightHandler fight.Handler

	statusMux sync.RWMutex
	// Errors from the Updater
	validationErrs status.MultiError
	applyErrs      status.MultiError
	watchErrs      status.MultiError
}

// NewSyncErrorCache constructs a new SyncErrorCache with shared handlers
func NewSyncErrorCache(conflictHandler conflict.Handler, fightHandler fight.Handler) *SyncErrorCache {
	return &SyncErrorCache{
		conflictHandler: conflictHandler,
		fightHandler:    fightHandler,
	}
}

// ConflictHandler returns the thread-safe handler of resource & management fights
func (s *SyncErrorCache) ConflictHandler() conflict.Handler {
	return s.conflictHandler
}

// FightHandler returns the thread-safe handler of controller fights
func (s *SyncErrorCache) FightHandler() fight.Handler {
	return s.fightHandler
}

// Errors returns the latest known set of errors from the updater and remediator.
func (s *SyncErrorCache) Errors() status.MultiError {
	s.statusMux.RLock()
	defer s.statusMux.RUnlock()

	// Ordering here is important. It needs to be the same as Updater.Update.
	var errs status.MultiError
	for _, conflictErr := range s.conflictHandler.ConflictErrors() {
		errs = status.Append(errs, conflictErr)
	}
	for _, fightErr := range s.fightHandler.FightErrors() {
		errs = status.Append(errs, fightErr)
	}
	errs = status.Append(errs, s.validationErrs)
	errs = status.Append(errs, s.applyErrs)
	errs = status.Append(errs, s.watchErrs)
	return errs
}

// SetValidationErrs replaces the cached validation errors
func (s *SyncErrorCache) SetValidationErrs(errs status.MultiError) {
	s.statusMux.Lock()
	defer s.statusMux.Unlock()
	s.validationErrs = errs
}

// AddApplyError adds an apply error to the list of cached apply errors.
func (s *SyncErrorCache) AddApplyError(err status.Error) {
	s.statusMux.Lock()
	defer s.statusMux.Unlock()
	s.applyErrs = status.Append(s.applyErrs, err)
}

// ResetApplyErrors deletes all cached apply errors.
func (s *SyncErrorCache) ResetApplyErrors() {
	s.statusMux.Lock()
	defer s.statusMux.Unlock()
	s.applyErrs = nil
}

// SetWatchErrs replaces the cached watch errors.
// These come from updating the watches, not watch event errors.
func (s *SyncErrorCache) SetWatchErrs(errs status.MultiError) {
	s.statusMux.Lock()
	defer s.statusMux.Unlock()
	s.watchErrs = errs
}
