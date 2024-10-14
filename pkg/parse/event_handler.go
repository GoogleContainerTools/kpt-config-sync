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
	Parser            Parser
	ReconcilerState   *reconcilerState
	NSControllerState *namespacecontroller.State
	Run               RunFunc
}

// NewEventHandler builds an EventHandler
func NewEventHandler(ctx context.Context, parser Parser, nsControllerState *namespacecontroller.State, runFn RunFunc) *EventHandler {
	return &EventHandler{
		Context:           ctx,
		Parser:            parser,
		ReconcilerState:   &reconcilerState{},
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
	opts := s.Parser.options()

	var eventResult events.Result
	// Wrap the RunFunc to set Result.RunAttempted.
	// This delays status update and sync events.
	runFn := func(ctx context.Context, p Parser, trigger string, state *reconcilerState) RunResult {
		result := s.Run(ctx, p, trigger, state)
		eventResult.RunAttempted = true
		return result
	}

	var runResult RunResult
	switch event.Type {
	case events.SyncWithReimportEventType:
		// Re-apply even if no changes have been detected.
		// This case should be checked first since it resets the cache.
		// If the reconciler is in the process of reconciling a given commit, the resync won't
		// happen until the ongoing reconciliation is done.
		klog.Infof("It is time for a force-resync")
		// Reset the cache partially to make sure all the steps of a parse-apply-watch loop will run.
		// The cached sourceState will not be reset to avoid reading all the source files unnecessarily.
		// The cached needToRetry will not be reset to avoid resetting the backoff retries.
		s.ReconcilerState.resetPartialCache()
		runResult = runFn(s.Context, s.Parser, triggerResync, s.ReconcilerState)

	case events.SyncEventType:
		// Re-import declared resources from the filesystem (from *-sync).
		// If the reconciler is in the process of reconciling a given commit, the re-import won't
		// happen until the ongoing reconciliation is done.
		runResult = runFn(s.Context, s.Parser, triggerReimport, s.ReconcilerState)

	case events.StatusEventType:
		// Publish the sync status periodically to update remediator errors.
		// Skip updates if the remediator is not running yet, paused, or watches haven't been updated yet.
		// This implies that this reconciler has successfully parsed, rendered, validated, and synced.
		if opts.Remediating() {
			klog.V(3).Info("Updating sync status (periodic while not syncing)")
			// Don't update the sync spec or commit.
			if err := setSyncStatus(s.Context, s.Parser, s.ReconcilerState, s.ReconcilerState.status.SyncStatus.Spec, false, s.ReconcilerState.status.SyncStatus.Commit, s.Parser.SyncErrors()); err != nil {
				if errors.Is(err, context.Canceled) {
					klog.Infof("Sync status update skipped: %v", err)
				} else {
					klog.Warningf("Failed to update sync status: %v", err)
				}
			}
		}

	case events.NamespaceResyncEventType:
		// If the namespace controller indicates that an update is needed,
		// attempt to re-sync.
		if !s.NSControllerState.ScheduleSync() {
			// No RunFunc call
			break
		}

		klog.Infof("A new sync is triggered by a Namespace event")
		// Reset the cache partially to make sure all the steps of a parse-apply-watch loop will run.
		// The cached sourceState will not be reset to avoid reading all the source files unnecessarily.
		// The cached needToRetry will not be reset to avoid resetting the backoff retries.
		s.ReconcilerState.resetPartialCache()
		runResult = runFn(s.Context, s.Parser, namespaceEvent, s.ReconcilerState)

	case events.RetrySyncEventType:
		// Retry if there was an error, conflict, or any watches need to be updated.
		var trigger string
		if opts.HasManagementConflict() {
			// Reset the cache partially to make sure all the steps of a parse-apply-watch loop will run.
			// The cached sourceState will not be reset to avoid reading all the source files unnecessarily.
			// The cached needToRetry will not be reset to avoid resetting the backoff retries.
			s.ReconcilerState.resetPartialCache()
			trigger = triggerManagementConflict
		} else if s.ReconcilerState.cache.needToRetry {
			trigger = triggerRetry
		} else if opts.needToUpdateWatch() {
			trigger = triggerWatchUpdate
		} else {
			// No RunFunc call
			break
		}

		// Set TriggerRetryBackoff to true so the RetrySyncPublisher can publish
		// retry events with backoff.
		eventResult.TriggerRetryBackoff = true

		// During the execution of `run`, if a new commit is detected,
		// retryTimer will be reset to `Options.RetryPeriod`, and state.backoff is reset to `defaultBackoff()`.
		// In this case, `run` will try to sync the configs from the new commit instead of the old commit
		// being retried.
		runResult = runFn(s.Context, s.Parser, trigger, s.ReconcilerState)

	default:
		klog.Fatalf("Invalid event received: %#v", event)
	}

	// If the run succeeded or source changed, reset the retry backoff.
	if runResult.Success || runResult.SourceChanged {
		eventResult.ResetRetryBackoff = true
		eventResult.TriggerRetryBackoff = false
	}
	return eventResult
}
