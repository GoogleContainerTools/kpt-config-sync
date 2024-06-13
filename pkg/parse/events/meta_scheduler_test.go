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
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	testingclock "k8s.io/utils/clock/testing"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

func TestMetaScheduler_ScheduleAndExecute(t *testing.T) {
	type eventResult struct {
		Event  Event
		Result HandleResult
	}
	tests := []struct {
		name           string
		scheduler      *MetaScheduler
		stepSize       time.Duration
		expectedEvents []eventResult
	}{
		{
			name: "SyncEvents From PollingPeriod",
			scheduler: &MetaScheduler{
				SyncPeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  SyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  SyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  SyncEvent{},
					Result: HandleResult{},
				},
			},
		},
		{
			name: "ResyncEvents From ResyncPeriod",
			scheduler: &MetaScheduler{
				FullSyncPeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  FullSyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  FullSyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  FullSyncEvent{},
					Result: HandleResult{},
				},
			},
		},
		{
			name: "StatusEvents From StatusUpdatePeriod",
			scheduler: &MetaScheduler{
				StatusUpdatePeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  StatusEvent{},
					Result: HandleResult{},
				},
				{
					Event:  StatusEvent{},
					Result: HandleResult{},
				},
				{
					Event:  StatusEvent{},
					Result: HandleResult{},
				},
			},
		},
		{
			name: "NamespaceResyncEvents From NamespaceControllerPeriod",
			scheduler: &MetaScheduler{
				NamespaceControllerPeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  NamespaceResyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  NamespaceResyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  NamespaceResyncEvent{},
					Result: HandleResult{},
				},
			},
		},
		{
			name: "RetryEvents From RetryBackoff",
			scheduler: &MetaScheduler{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   0, // no backoff, just 1s delay
					Steps:    3, // at least as many as we expect
				},
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  RetrySyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  RetrySyncEvent{},
					Result: HandleResult{},
				},
				{
					Event:  RetrySyncEvent{},
					Result: HandleResult{},
				},
			},
		},
		{
			name: "RetryEvents From RetryBackoff with ResetRetryBackoff",
			scheduler: &MetaScheduler{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   0, // no backoff, just 1s delay
					Steps:    1, // just one, to show the count resets when ResetRetryBackoff is true
				},
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event: RetrySyncEvent{},
					Result: HandleResult{
						ResetRetryBackoff: true,
					},
				},
				{
					Event: RetrySyncEvent{},
					Result: HandleResult{
						ResetRetryBackoff: true,
					},
				},
				{
					Event: RetrySyncEvent{},
					Result: HandleResult{
						ResetRetryBackoff: true,
					},
				},
			},
		},
		// TODO: test retry backoff behavior
		// TODO: test all handlers together (can cause race conditions)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var expectedEvents, receivedEvents []Event
			for _, e := range tc.expectedEvents {
				expectedEvents = append(expectedEvents, e.Event)
			}

			fakeClock := testingclock.NewFakeClock(time.Now().Truncate(time.Second))
			tc.scheduler.Clock = fakeClock

			eventIndex := 0

			handleFn := func(e Event) HandleResult {
				t.Logf("Received event: %#v", e)
				// Record received events
				receivedEvents = append(receivedEvents, e)
				// Handle unexpected extra events
				if eventIndex >= len(tc.expectedEvents) {
					return HandleResult{}
				}
				// Handle expected events
				result := tc.expectedEvents[eventIndex]
				eventIndex++
				// If all expected events have been seen, stop the generator
				if eventIndex >= len(tc.expectedEvents) {
					t.Log("Stopping")
					cancel()
				} else {
					// Move clock forward, without blocking return
					go func() {
						t.Log("Step")
						fakeClock.Step(tc.stepSize)
					}()
				}
				return result.Result
			}
			eventStreams := tc.scheduler.Schedule(ctx)

			// Move clock forward, without blocking handler
			go func() {
				t.Log("First Step")
				fakeClock.Step(tc.stepSize)
			}()

			Execute(ctx, eventStreams, handleFn)

			testutil.AssertEqual(t, expectedEvents, receivedEvents)
		})
	}
}
