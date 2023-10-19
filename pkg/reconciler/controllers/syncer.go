// Copyright 2023 Google LLC
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

package controllers

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/parse"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// Syncer is a controller that watches for timer-based Syncer requests, and also
// watches for the Namespace if the Namespace watcher is enabled.
// It reconciles the Syncer requests by reading from the source configs, parse,
// validate and apply them to the cluster, and then start a watcher for the
// GKNN of each declared resource.
type Syncer struct {
	// controllerCtx is set in reconciler/main.go and is canceled by the Finalizer.
	controllerCtx context.Context

	// doneCh indicates the Syncer stops processing new requests.
	// It is a signal for the Finalizer to continue to finalize managed resources.
	doneCh chan struct{}

	// scope is the scope of the reconciler, either a namespace or ':root'
	scope declared.Scope

	// pollingPeriod is the period of time between checking the filesystem for
	// source updates to sync.
	pollingPeriod time.Duration

	// ResyncPeriod is the period of time between forced re-sync from source
	// (even without a new commit).
	resyncPeriod time.Duration

	// retryPeriod is how long the parser waits between retries, after an error.
	retryPeriod time.Duration

	// statusUpdatePeriod is how long the parser waits between updates of the
	// sync status, to account for management conflict errors from the remediator.
	statusUpdatePeriod time.Duration

	reimportTimer     *time.Timer
	resyncTimer       *time.Timer
	retryTimer        *time.Timer
	statusUpdateTimer *time.Timer

	// parser is the actor that parses the source configs from file format to unstructured Kubernetes objects,
	// and sends K8s API calls to set the status field. It also contains an updater
	// that applies the declared resources to the cluster, and update watches for drift correction.
	parser parse.Parser

	// state stores the shared state across multiple Syncer reconciliations.
	state *parse.ReconcilerState

	// wg is the WaitGroup for the Syncer.
	wg sync.WaitGroup

	// lifecycleMux guards live status of the Syncer.
	lifecycleMux sync.Mutex
	// canSync indicates whether the Syncer can start processing requests.
	canSync bool
}

// NewSyncer initializes the Syncer controller.
func NewSyncer(controlCtx context.Context, scope declared.Scope, parser parse.Parser,
	state *parse.ReconcilerState, pollingPeriod, resyncPeriod, retryPeriod,
	statusUpdatePeriod time.Duration, doneCh chan struct{}) *Syncer {
	return &Syncer{
		controllerCtx:      controlCtx,
		scope:              scope,
		pollingPeriod:      pollingPeriod,
		resyncPeriod:       resyncPeriod,
		retryPeriod:        retryPeriod,
		statusUpdatePeriod: statusUpdatePeriod,
		reimportTimer:      time.NewTimer(pollingPeriod),
		resyncTimer:        time.NewTimer(resyncPeriod),
		retryTimer:         time.NewTimer(retryPeriod),
		statusUpdateTimer:  time.NewTimer(statusUpdatePeriod),
		parser:             parser,
		state:              state,
		doneCh:             doneCh,
		canSync:            true, // The syncer should be able to sync at any time unless the controller context is canceled.
	}
}

// Reconcile processes the Syncer request to apply the source configs to the cluster.
func (s *Syncer) Reconcile(_ context.Context, req ctrl.Request) (ctrl.Result, error) {
	if errors.Is(s.controllerCtx.Err(), context.Canceled) {
		// Drain the request queue
		klog.V(3).Infof("Stop syncing new events as the context is canceled..")
		return ctrl.Result{}, nil
	}

	trigger := parse.Trigger(req.Name)
	// Indicate the reconcile function is running to prevent other threads from
	// interrupting the reconciliation.
	if !s.StartSyncing() {
		klog.V(1).Infof("Skip processing the %s event because the Syncer is paused", trigger)
		return ctrl.Result{}, nil
	}
	// Indicate the current reconciliation is done to unblock other threads that
	// are waiting for the completion.
	defer s.DoneSyncing()

	switch trigger {
	case parse.Reimport:
		klog.V(3).Infof("New syncer reconciliation triggered by %q", trigger)
		parse.Run(s.controllerCtx, s.parser, trigger, s.state)
		s.reimportTimer.Reset(s.pollingPeriod) // Schedule reimport attempt
		s.retryTimer.Reset(s.retryPeriod)      // Schedule retry attempt
	case parse.Resync:
		klog.Infof("New syncer reconciliation triggered by %q", trigger)
		// Reset the cache partially to make sure all the steps of a parse-apply-watch loop will run.
		// The cached sourceState will not be reset to avoid reading all the source files unnecessarily.
		// The cached needToRetry will not be reset to avoid resetting the backoff retries.
		s.state.ResetPartialCache()
		parse.Run(s.controllerCtx, s.parser, trigger, s.state)
		s.resyncTimer.Reset(s.resyncPeriod) // Schedule resync attempt
		s.retryTimer.Reset(s.retryPeriod)   // Schedule retry attempt
	case parse.ManagementConflict:
		klog.Infof("New syncer reconciliation triggered by %q", trigger)
		// The ManagementConflict request is processed asynchronously after it is enqueued.
		// The conflict may have been resolved while processing other request, so
		// it checks again before execution to avoid unnecessary retries.
		if !s.parser.HasManagementConflict() {
			klog.Infof("The managementConflict issue has been resolved")
			s.retryTimer.Reset(s.retryPeriod) // Schedule retry attempt
			return ctrl.Result{}, nil
		}
		s.retry(trigger, true)
	case parse.Retry:
		klog.Infof("New syncer reconciliation triggered by %q", trigger)
		// The retry request is processed asynchronously after it is enqueued.
		// The issue may have been resolved while processing other request, so
		// it checks again before execution to avoid unnecessary retries.
		if !s.state.NeedRetry() {
			klog.Infof("The sync failure has been resolved")
			s.retryTimer.Reset(s.retryPeriod) // Schedule retry attempt
			return ctrl.Result{}, nil
		}
		s.retry(trigger, false)
	case parse.WatchUpdate:
		klog.Infof("New syncer reconciliation triggered by %q", trigger)
		// The WatchUpdate request is processed asynchronously after it is enqueued.
		// The issue may have been resolved while processing other request, so
		// it checks again before execution to avoid unnecessary retries.
		if !s.parser.NeedWatchUpdate() {
			klog.Infof("The watchUpdate issue has been resolved")
			s.retryTimer.Reset(s.retryPeriod) // Schedule retry attempt
			return ctrl.Result{}, nil
		}
		s.retry(trigger, false)
	case parse.StatusUpdate:
		klog.V(3).Info("Updating sync status (periodic while not syncing)")
		// Skip sync status update if the .status.sync.commit is out of date.
		// This avoids overwriting a newer Syncing condition with the status
		// from an older commit.
		if s.state.SyncCommitInStatus() == s.state.SourceCommitInStatus() &&
			s.state.SyncCommitInStatus() == s.state.RenderingCommitInStatus() {
			if err := parse.SetSyncStatus(s.controllerCtx, s.parser, s.state, s.parser.Syncing(), s.parser.SyncErrors()); err != nil {
				klog.Warningf("failed to update sync status: %v", err)
			}
		}
		s.statusUpdateTimer.Reset(s.statusUpdatePeriod) // Schedule status update attempt
		s.retryTimer.Reset(s.retryPeriod)               // Schedule retry attempt
	default:
		klog.Errorf("unknown trigger %s", trigger)
		s.statusUpdateTimer.Reset(s.statusUpdatePeriod) // Schedule status update attempt
		s.retryTimer.Reset(s.retryPeriod)               // Schedule retry attempt
	}

	// No requeueing for the run failures because of the following reasons:
	// 1. It only requeues run failures, but there are other failures that need to be
	//    retried, e.g. management conflicts and updateWatch signal from the remediator.
	// 2. the controller-runtime requeues the request using the default exponential
	//    backoff, which is not tunable by Config Sync.
	// 3. The default requeue can't filter out obsolete events.
	return ctrl.Result{}, nil
}

// SetupWithManager registers the Syncer Controller.
func (s *Syncer) SetupWithManager(mgr ctrl.Manager) error {
	// an event channel to trigger reconciliation
	timerEvents := make(chan event.GenericEvent)

	// run the timer as a goroutine to trigger the reconciliation periodically
	go func() {
		// Use timers, not tickers.
		// Tickers can cause memory leaks and continuous execution, when execution
		// takes longer than the tick duration.
		defer func() {
			s.reimportTimer.Stop()
			s.resyncTimer.Stop()
			s.retryTimer.Stop()
			s.statusUpdateTimer.Stop()
		}()

		s.state.InitRetryTimerAndPeriod(s.retryTimer, s.retryPeriod)
		for {
			select {
			case <-s.controllerCtx.Done():
				// The controller is shutting down, close the timerEvents channel and exit
				close(timerEvents)
				// Stop enqueueing and executing new syncing requests.
				s.PauseSyncing()
				// block and wait until the current syncing is done.
				s.WaitSyncing()
				close(s.doneCh) // inform finalizer the syncer is done
				return

			// Re-apply even if no changes have been detected.
			// This case should be checked first since it resets the cache.
			// If the reconciler is in the process of reconciling a given commit, the resync won't
			// happen until the ongoing reconciliation is done.
			case <-s.resyncTimer.C:
				timerEvents <- timerEvent(parse.Resync)

			// Re-import declared resources from the filesystem.
			// If the reconciler is in the process of reconciling a given commit, the
			// re-import won't happen until the ongoing reconciliation is done.
			case <-s.reimportTimer.C:
				timerEvents <- timerEvent(parse.Reimport)

			// Retry if there was an error, conflict, or any watches need to be updated.
			case <-s.retryTimer.C:
				var trigger parse.Trigger
				if s.parser.HasManagementConflict() {
					trigger = parse.ManagementConflict
				} else if s.state.NeedRetry() {
					trigger = parse.Retry
				} else if s.parser.NeedWatchUpdate() {
					trigger = parse.WatchUpdate
				} else {
					// If no failures to retry, skip without resetting the retry timer
					// The retry timer will be reset after a reconciliation is done.
					continue
				}
				timerEvents <- timerEvent(trigger)

			// Update the sync status to report management conflicts (from the remediator).
			case <-s.statusUpdateTimer.C:
				timerEvents <- timerEvent(parse.StatusUpdate)
			}
		}
	}()

	ctrlBuilder := ctrl.NewControllerManagedBy(mgr).
		Named(SyncerController).
		// watch for the timer events
		Watches(&source.Channel{Source: timerEvents},
			&handler.EnqueueRequestForObject{})

	return ctrlBuilder.Complete(s)
}

func timerEvent(trigger parse.Trigger) event.GenericEvent {
	// Enqueue the request as a Kubernetes core Event
	req := corev1.Event{}
	req.Name = string(trigger)
	// Send the Event to the event channel to trigger a new reconciliation.
	return event.GenericEvent{Object: &req}
}

// retry re-runs the parse-apply-watch loop for the given retry trigger (managementConflict, retry, or watchUpdate).
// If resetCache is true, it clears the cache to force the parse-apply-watch loop to run.
// In the end, it sets the next retryTimer with exponential backoff.
// If it reaches retry, the retryTimer won't be reset, so no more retries will be scheduled.
func (s *Syncer) retry(trigger parse.Trigger, resetCache bool) {
	if s.state.ReachRetryLimit() {
		klog.Infof("Retry limit (%d) has been reached. No more retries will be scheduled for the same commit.", parse.RetryLimit)
		// Don't reset retryTimer if retry limit has been reached.
		// retryBackoff and retryTimer will be reset when a new commit is detected.
	} else {
		retryDuration := s.state.NextRetryDuration()
		retries := parse.RetryLimit - s.state.RetrySteps()
		klog.Infof("A retry is triggered (trigger: %s, retries: %d/%d), next retry will be scheduled in %s",
			trigger, retries, parse.RetryLimit, retryDuration)
		// Reset the cache partially to make sure all the steps of a parse-apply-watch loop will run.
		// The cached sourceState will not be reset to avoid reading all the source files unnecessarily.
		// The cached needToRetry will not be reset to avoid resetting the backoff retries.
		if resetCache {
			s.state.ResetPartialCache()
		}
		parse.Run(s.controllerCtx, s.parser, trigger, s.state)
		s.retryTimer.Reset(retryDuration) // Schedule retry attempt
	}
}

// StartSyncing marks the Syncer is working.
func (s *Syncer) StartSyncing() bool {
	s.lifecycleMux.Lock()
	defer s.lifecycleMux.Unlock()

	if s.canSync {
		klog.V(1).Infof("Start syncing...")
		s.wg.Add(1)
	}
	return s.canSync
}

// WaitSyncing blocks until the Syncer finishes processing the requests.
func (s *Syncer) WaitSyncing() {
	klog.V(1).Info("Waiting for the Syncer to complete...")
	s.wg.Wait()
}

// DoneSyncing marks the Syncer as done when it finishes syncing.
func (s *Syncer) DoneSyncing() {
	klog.V(1).Info("Syncer completes...")
	s.wg.Done()
}

// PauseSyncing stops the Syncer to process new requests.
func (s *Syncer) PauseSyncing() {
	s.lifecycleMux.Lock()
	defer s.lifecycleMux.Unlock()

	klog.V(1).Info("Syncer pausing processing new request...")
	s.canSync = false
	klog.V(3).Info("Syncer paused processing new request")
}
