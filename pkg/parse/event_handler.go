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
}

// NewEventHandler builds an EventHandler
func NewEventHandler(ctx context.Context, r Reconciler, nsControllerState *namespacecontroller.State) *EventHandler {
	return &EventHandler{
		Context:           ctx,
		Reconciler:        r,
		NSControllerState: nsControllerState,
	}
}

// Handle an Event and return the Result.
// - FullSyncEventType      - Reset the cache and sync from scratch.
// - SyncEventType          - Sync from the cache, priming the cache from disk, if necessary.
// - StatusUpdateEventType  - Update the RSync status with status from the Remediator & NSController.
// - NamespaceSyncEventType - Sync from the cache, if the NSController requested one.
// - RetrySyncEventType     - Sync from the cache, if one of the following cases is detected:
//   - Remediator or Reconciler reported a management conflict
//   - Reconciler requested a retry due to error
//   - Remediator requested a watch update
func (s *EventHandler) Handle(event events.Event) events.Result {
	ctx := s.Context
	opts := s.Reconciler.Options()
	state := s.Reconciler.ReconcilerState()

	var eventResult events.Result
	// Wrap the RunFunc to set Result.RunAttempted.
	// This delays status update and sync events.
	runFn := func(ctx context.Context, trigger string) ReconcileResult {
		result := s.Reconciler.Reconcile(ctx, trigger)
		eventResult.RunAttempted = true
		return result
	}

	var runResult ReconcileResult
	switch event.Type {
	case events.SyncEventType:
		runResult = runFn(ctx, triggerSync)

	case events.StatusUpdateEventType:
		// Publish the sync status periodically to update remediator errors.
		if err := s.Reconciler.UpdateSyncStatus(ctx); err != nil {
			if errors.Is(err, context.Canceled) {
				klog.Infof("Sync status update skipped: %v", err)
			} else {
				klog.Warningf("Failed to update sync status: %v", err)
			}
		}

	case events.NamespaceSyncEventType:
		// FullSync if the namespace controller detected a change.
		if !s.NSControllerState.ScheduleSync() {
			// No RunFunc call
			break
		}
		runResult = runFn(ctx, triggerNamespaceUpdate)

	case events.RetrySyncEventType:
		// Retry if there was an error, conflict, or any watches need to be updated.
		var trigger string
		if opts.HasManagementConflict() {
			trigger = triggerManagementConflict
		} else if state.cache.needToRetry {
			trigger = triggerRetry
		} else if opts.needToUpdateWatch() {
			trigger = triggerWatchUpdate
		} else {
			// Skip RunFunc and reset the backoff to keep checking for conflicts & watch updates.
			klog.V(3).Info("Sync retry skipped; resetting retry backoff")
			eventResult.ResetRetryBackoff = true
			break
		}

		// During the execution of `run`, if a new commit is detected,
		// retryTimer will be reset to `Options.RetryPeriod`, and state.backoff is reset to `defaultBackoff()`.
		// In this case, `run` will try to sync the configs from the new commit instead of the old commit
		// being retried.
		runResult = runFn(ctx, trigger)

	default:
		klog.Fatalf("Invalid event received: %#v", event)
	}

	// If the run succeeded or source changed, reset the retry backoff.
	if runResult.Success {
		klog.V(3).Info("Sync attempt succeeded; resetting retry backoff")
		eventResult.ResetRetryBackoff = true
	} else if runResult.SourceChanged {
		klog.V(3).Info("Source change detected; resetting retry backoff")
		eventResult.ResetRetryBackoff = true
	}
	return eventResult
}
