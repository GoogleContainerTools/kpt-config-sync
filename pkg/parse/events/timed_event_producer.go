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

// TimedEventProducer is an event producer that implements the GenerateFunc
// interface as a method. Most of the events are driven by timers (for now).
// This is because the previous implementation used timers. The driver for these
// events may change in the future with subsequent refactors to make it more
// event driven.
type TimedEventProducer struct {
	// Clock is used for time tracking, namely to simplify testing by allowing
	// a fake clock, instead of a RealClock.
	Clock clock.Clock
	// PollingPeriod is the period of time between checking the filesystem for
	// source updates to sync.
	PollingPeriod time.Duration
	// ResyncPeriod is the period of time between forced re-sync from source
	// (even without a new commit).
	ResyncPeriod time.Duration
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

// Generate generates and sends events to the HandleEventFunc based on the
// configured time periods, until the context is done.
// Generate implements GenerateFunc.
func (t *TimedEventProducer) Generate(ctx context.Context, handleFn HandleEventFunc) {
	// Use timers, not tickers.
	// Tickers can cause memory leaks and continuous execution, when execution
	// takes longer than the tick duration.

	type EventHandler struct {
		// Name of the handler
		Name string
		// Chan takes a readable channel value.
		// Reflection is used to allow channels with any payload type.
		// Usage: reflect.ValueOf(ctx.Done())
		Chan reflect.Value
		// Handle handles an event without input. Returns a HandleResult.
		Handle func() HandleResult
	}

	var handlers []EventHandler

	// Context handler
	klog.V(3).Info("Starting Context generator")
	done := false
	handlers = append(handlers, EventHandler{
		Name: "Context",
		Chan: reflect.ValueOf(ctx.Done()),
		Handle: func() HandleResult {
			done = true
			return HandleResult{}
		},
	})

	// ResyncEvent generator
	klog.V(5).Info("Starting ResyncEvent generator")
	resyncTimer := t.Clock.NewTimer(t.ResyncPeriod)
	defer resyncTimer.Stop()
	handlers = append(handlers, EventHandler{
		Name: ResyncEvent{}.Type(),
		Chan: reflect.ValueOf(resyncTimer.C()),
		Handle: func() HandleResult {
			result := handleFn(ResyncEvent{})

			// Schedule next resync attempt
			resyncTimer.Reset(t.ResyncPeriod)
			return result
		},
	})

	// SyncEvent generator
	klog.V(5).Info("Starting SyncEvent generator")
	runTimer := t.Clock.NewTimer(t.PollingPeriod)
	defer runTimer.Stop()
	handlers = append(handlers, EventHandler{
		Name: SyncEvent{}.Type(),
		Chan: reflect.ValueOf(runTimer.C()),
		Handle: func() HandleResult {
			result := handleFn(SyncEvent{})

			// Schedule next sync attempt
			runTimer.Reset(t.PollingPeriod)
			return result
		},
	})

	// RetryEvent generator
	klog.V(5).Info("Starting RetryEvent generator")
	backoff := util.CopyBackoff(t.RetryBackoff)
	retryLimit := backoff.Steps
	retryTimer := t.Clock.NewTimer(backoff.Duration)
	defer retryTimer.Stop()
	handlers = append(handlers, EventHandler{
		Name: RetryEvent{}.Type(),
		Chan: reflect.ValueOf(retryTimer.C()),
		Handle: func() HandleResult {
			if backoff.Steps == 0 {
				klog.Infof("Retry limit (%v) has been reached", retryLimit)
				// Don't reset retryTimer if retry limit has been reached.
				return HandleResult{}
			}

			event := RetryEvent{
				ResetRetryBackoff: func() {
					backoff = util.CopyBackoff(t.RetryBackoff)
					retryTimer.Reset(backoff.Duration)
				},
			}

			retryDuration := backoff.Step()
			retries := retryLimit - backoff.Steps
			klog.Infof("a retry is triggered (retries: %v/%v)", retries, retryLimit)

			result := handleFn(event)

			// Schedule next retry attempt
			retryTimer.Reset(retryDuration)
			return result
		},
	})

	// StatusEvent generator
	klog.V(5).Info("Starting StatusEvent generator")
	statusUpdateTimer := t.Clock.NewTimer(t.StatusUpdatePeriod)
	defer statusUpdateTimer.Stop()
	handlers = append(handlers, EventHandler{
		Name: StatusEvent{}.Type(),
		Chan: reflect.ValueOf(statusUpdateTimer.C()),
		Handle: func() HandleResult {
			result := handleFn(StatusEvent{})

			// Schedule next status update attempt
			result.DelayStatusUpdate = true
			return result
		},
	})

	// NamespaceResyncEvent generator (RootSync only)
	if t.NamespaceControllerPeriod > 0 {
		klog.V(5).Info("Starting NamespaceResyncEvent generator")
		nsEventTimer := t.Clock.NewTimer(t.NamespaceControllerPeriod)
		defer nsEventTimer.Stop()
		handlers = append(handlers, EventHandler{
			Name: NamespaceResyncEvent{}.Type(),
			Chan: reflect.ValueOf(nsEventTimer.C()),
			Handle: func() HandleResult {
				result := handleFn(NamespaceResyncEvent{})

				// Schedule next namespace event check attempt
				nsEventTimer.Reset(t.NamespaceControllerPeriod)
				return result
			},
		})
	}

	// Convert the list of EventHandlers into parallel lists with the same index.
	cases := make([]reflect.SelectCase, len(handlers))
	caseNames := make([]string, len(handlers))
	caseHandlers := make([]func() HandleResult, len(handlers))
	for i, handler := range handlers {
		cases[i] = reflect.SelectCase{
			Dir:  reflect.SelectRecv,
			Chan: handler.Chan,
		}
		caseNames[i] = handler.Name
		caseHandlers[i] = handler.Handle
	}

	// Select from cases until done
	remaining := len(cases)
	for remaining > 0 {
		// Use reflect.Select to allow a dynamic set of cases.
		// None of the channels return anything important, so ignore the value.
		caseIndex, _, ok := reflect.Select(cases)
		if !ok {
			// Closed channels are always selected, so nil the channel to
			// disable this case. We can't just remove the case from the list,
			// because we need the index to match the handlers list.
			cases[caseIndex].Chan = reflect.ValueOf(nil)
			remaining--
			continue
		}
		klog.V(5).Infof("Handling case[%d]: %s", caseIndex, caseNames[caseIndex])
		result := caseHandlers[caseIndex]()
		if done {
			return
		}
		if result.DelayStatusUpdate {
			// Delay next status update attempt
			statusUpdateTimer.Reset(t.StatusUpdatePeriod)
		}
	}
}
