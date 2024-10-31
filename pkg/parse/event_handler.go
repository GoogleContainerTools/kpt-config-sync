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
	"context"
	"errors"

	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/parse/events"
	"kpt.dev/configsync/pkg/reconciler/namespacecontroller"
)

// EventHandler is a events.Subscriber implementation that handles events and
// triggers the RunFunc when appropriate.
type EventHandler struct {
	Context           context.Context
	Reconciler        Reconciler
	NSControllerState *namespacecontroller.State
	Run               RunFunc
}

// NewEventHandler builds an EventHandler
func NewEventHandler(ctx context.Context, r Reconciler, nsControllerState *namespacecontroller.State, runFn RunFunc) *EventHandler {
	return &EventHandler{
		Context:           ctx,
		Reconciler:        r,
		NSControllerState: nsControllerState,
		Run:               runFn,
	}
}

// Handle an Event and return the Result.
// - SyncWithReimportEventType - Reset the cache and sync from scratch.
// - SyncEventType             - Sync from the cache, priming the cache from disk, if necessary.
// - StatusEventType           - Update the RSync status with status from the Remediator & NSController.
// - NamespaceResyncEventType  - Sync from the cache, if the NSController requested one.
// - RetrySyncEventType        - Sync from the cache, if one of the following cases is detected:
//   - Remediator or Reconciler reported a management conflict
//   - Reconciler requested a retry due to error
//   - Remediator requested a watch update
func (s *EventHandler) Handle(event events.Event) events.Result {
	opts := s.Reconciler.Options()
	state := s.Reconciler.ReconcilerState()

	var eventResult events.Result
	// Wrap the RunFunc to set Result.RunAttempted.
	// This delays status update and sync events.
	runFn := func(ctx context.Context, r Reconciler, trigger string) RunResult {
		result := s.Run(ctx, r, trigger)
		eventResult.RunAttempted = true
		return result
	}

	var runResult RunResult
	switch event.Type {
	case events.SyncWithReimportEventType:
		// Reimport = Read* + Render* + Parse + Update
		//
		// * Read & Render will only happen if there's a new commit, new source
		//   spec change, or a previous error invalidated the cache.
		//   Otherwise re-import starts from re-parsing the objects from disk.
		runResult = runFn(s.Context, s.Reconciler, triggerReimport)

	case events.SyncEventType:
		// Resync = Read* + Render* + Parse* + Update
		//
		// * Read, Render, and Parse will only happen if there's a new commit,
		//   new source spec change, or a previous error invalidated the cache.
		//   Otherwise re-sync skips directly to the Update stage, using the
		//   previously parsed in-memory object cache.
		runResult = runFn(s.Context, s.Reconciler, triggerResync)

	case events.StatusEventType:
		// Publish the sync status periodically to update remediator errors.
		// Skip updates if the remediator is not running yet, paused, or watches haven't been updated yet.
		// This implies that this reconciler has successfully parsed, rendered, validated, and synced.
		if opts.Remediating() {
			klog.V(3).Info("Updating sync status (periodic while not syncing)")
			// Don't update the sync spec or commit, just the errors and status.
			syncStatus := &SyncStatus{
				Spec:       state.status.SyncStatus.Spec,
				Syncing:    false,
				Commit:     state.status.SyncStatus.Commit,
				Errs:       s.Reconciler.ReconcilerState().SyncErrors(),
				LastUpdate: nowMeta(opts),
			}
			if err := s.Reconciler.SetSyncStatus(s.Context, syncStatus); err != nil {
				if errors.Is(err, context.Canceled) {
					klog.Infof("Sync status update skipped: %v", err)
				} else {
					klog.Warningf("Failed to update sync status: %v", err)
				}
			}
		}

	case events.NamespaceResyncEventType:
		// Reimport & Resync if the namespace controller detected a change.
		if !s.NSControllerState.ScheduleSync() {
			// No RunFunc call
			break
		}
		runResult = runFn(s.Context, s.Reconciler, namespaceEvent)

	case events.RetrySyncEventType:
		// Resync if there was an error.
		if !state.cache.needToRetry {
			klog.Info("Skipping retry attempt: not needed")
			break
		}
		runResult = runFn(s.Context, s.Reconciler, triggerRetry)

	default:
		klog.Fatalf("Invalid event received: %#v", event)
	}

	// If the run succeeded or source changed, reset the retry backoff.
	if runResult.Success || runResult.SourceChanged {
		eventResult.ResetRetryBackoff = true
	}
	return eventResult
}
