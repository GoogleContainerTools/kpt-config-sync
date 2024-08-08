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

package events

import (
	"context"
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"kpt.dev/configsync/pkg/util"
)

// MetaScheduler is an event producer that implements the ScheduleFunc
// interface as a method. Most of the events are driven by timers (for now).
// This is because the previous implementation used timers. The driver for these
// events may change in the future with subsequent refactors to make it more
// channel driven.
type MetaScheduler struct {
	// Clock is used for time tracking, namely to simplify testing by allowing
	// a fake clock, instead of a RealClock.
	Clock clock.Clock
	// SyncPeriod is the period of time between checking the filesystem for
	// source updates to sync.
	SyncPeriod time.Duration
	// FullSyncPeriod is the period of time between forced re-sync from source
	// (even without a new commit).
	FullSyncPeriod time.Duration
	// StatusUpdatePeriod is how long the Parser waits between updates of the
	// sync status, to account for management conflict errors from the Remediator.
	StatusUpdatePeriod time.Duration
	// NamespaceControllerPeriod is how long to wait between checks to see if
	// the namespace-controller wants to trigger a resync.
	// TODO: Use a channel, instead of a timer checking a locked variable.
	NamespaceControllerPeriod time.Duration
	// RetryBackoff is how long the Parser waits between retries, after an error.
	RetryBackoff wait.Backoff
}

// Schedule generates a list of EventProducers based on the MetaScheduler config.
func (t *MetaScheduler) Schedule(ctx context.Context) []EventProducer {
	// Use timers, not tickers.
	// Tickers can cause memory leaks and continuous execution, when execution
	// takes longer than the tick duration.

	var streams []EventProducer

	if t.FullSyncPeriod > 0 {
		klog.Info("Starting FullSyncEvent generator")
		streams = append(streams, NewFullSyncStream(t.Clock, t.FullSyncPeriod))
	}

	if t.SyncPeriod > 0 {
		klog.Info("Starting SyncEvent generator")
		streams = append(streams, NewSyncStream(t.Clock, t.SyncPeriod))
	}

	if t.RetryBackoff.Duration > 0 {
		klog.Info("Starting RetrySyncEvent generator")
		streams = append(streams, NewRetrySyncStream(t.Clock, t.RetryBackoff))
	}

	if t.StatusUpdatePeriod > 0 {
		klog.Info("Starting StatusEvent generator")
		streams = append(streams, NewStatusUpdateStream(t.Clock, t.StatusUpdatePeriod))
	}

	if t.NamespaceControllerPeriod > 0 {
		klog.Info("Starting NamespaceResyncEvent generator")
		streams = append(streams, NewNamespaceEventStream(t.Clock, t.NamespaceControllerPeriod))
	}

	return streams
}

// stopFunc is the signature for Timer.Stop, which returns true if the timer was
// running.
type stopFunc func() bool

// toCancelFunc converts a stopFunc to a CancelFunc, ignoring the return value.
func toCancelFunc(fn stopFunc) context.CancelFunc {
	return func() {
		// ignore return bool
		_ = fn()
	}
}

// NewFullSyncStream constructs an EventProducer that generates and handles
// FullSyncEvents.
func NewFullSyncStream(c clock.Clock, period time.Duration) EventProducer {
	resyncTimer := c.NewTimer(period)
	return EventProducer{
		EventType: FullSyncEvent{}.Type(),
		EventChan: reflect.ValueOf(resyncTimer.C()),
		HandleEvent: func(handleFn HandleEventFunc) HandleResult {
			result := handleFn(FullSyncEvent{})

			// Schedule next resync attempt
			resyncTimer.Reset(period)
			return result
		},
		Stop: toCancelFunc(resyncTimer.Stop),
	}
}

// NewSyncStream constructs an EventProducer that generates and handles
// SyncEvents.
func NewSyncStream(c clock.Clock, period time.Duration) EventProducer {
	runTimer := c.NewTimer(period)
	return EventProducer{
		EventType: SyncEvent{}.Type(),
		EventChan: reflect.ValueOf(runTimer.C()),
		HandleEvent: func(handleFn HandleEventFunc) HandleResult {
			result := handleFn(SyncEvent{})

			// Schedule next sync attempt
			runTimer.Reset(period)
			return result
		},
		Stop: toCancelFunc(runTimer.Stop),
	}
}

// NewRetrySyncStream constructs an EventProducer that generates and handles
// RetrySyncEvents with retry backoff.
func NewRetrySyncStream(c clock.Clock, backoff wait.Backoff) EventProducer {
	currentBackoff := util.CopyBackoff(backoff)
	retryLimit := currentBackoff.Steps
	retryTimer := c.NewTimer(currentBackoff.Duration)
	return EventProducer{
		EventType: RetrySyncEvent{}.Type(),
		EventChan: reflect.ValueOf(retryTimer.C()),
		HandleEvent: func(handleFn HandleEventFunc) HandleResult {
			if currentBackoff.Steps == 0 {
				klog.Infof("Retry limit (%v) has been reached", retryLimit)
				// Don't reset retryTimer if retry limit has been reached.
				return HandleResult{}
			}

			retryDuration := currentBackoff.Step()
			retries := retryLimit - currentBackoff.Steps
			klog.Infof("a retry is triggered (retries: %v/%v)", retries, retryLimit)

			result := handleFn(RetrySyncEvent{})

			// Schedule next retry attempt
			retryTimer.Reset(retryDuration)
			return result
		},
		HandleResult: func(result HandleResult) {
			if result.ResetRetryBackoff {
				currentBackoff = util.CopyBackoff(backoff)
				retryTimer.Reset(currentBackoff.Duration)
			}
		},
		Stop: toCancelFunc(retryTimer.Stop),
	}
}

// NewStatusUpdateStream constructs an EventProducer that generates and handles
// StatusEvents.
func NewStatusUpdateStream(c clock.Clock, period time.Duration) EventProducer {
	statusUpdateTimer := c.NewTimer(period)
	return EventProducer{
		EventType: StatusEvent{}.Type(),
		EventChan: reflect.ValueOf(statusUpdateTimer.C()),
		HandleEvent: func(handleFn HandleEventFunc) HandleResult {
			result := handleFn(StatusEvent{})

			// Schedule next status update attempt
			result.DelayStatusUpdate = true
			return result
		},
		HandleResult: func(result HandleResult) {
			// If Run was called by a handler, it usually includes updating the
			// RSync status, in which case, we want to delay the next stats update.
			if result.DelayStatusUpdate {
				statusUpdateTimer.Reset(period)
			}
		},
		Stop: toCancelFunc(statusUpdateTimer.Stop),
	}
}

// NewNamespaceEventStream constructs an EventProducer that generates and
// handles NamespaceResyncEvents.
func NewNamespaceEventStream(c clock.Clock, period time.Duration) EventProducer {
	nsEventTimer := c.NewTimer(period)
	return EventProducer{
		EventType: NamespaceResyncEvent{}.Type(),
		EventChan: reflect.ValueOf(nsEventTimer.C()),
		HandleEvent: func(handleFn HandleEventFunc) HandleResult {
			result := handleFn(NamespaceResyncEvent{})

			// Schedule next namespace event check attempt
			nsEventTimer.Reset(period)
			return result
		},
		Stop: toCancelFunc(nsEventTimer.Stop),
	}
}
