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

func TestFunnel_Start(t *testing.T) {
	tests := []struct {
		name           string
		builder        *PublishingGroupBuilder
		stepSize       time.Duration
		steps          []time.Duration
		expectedEvents []eventResult
	}{
		{
			name: "SyncEvents From SyncPeriod",
			builder: &PublishingGroupBuilder{
				SyncPeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: SyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: SyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: SyncEventType},
					Result: Result{},
				},
			},
		},
		{
			name: "StatusEvents From StatusUpdatePeriod",
			builder: &PublishingGroupBuilder{
				StatusUpdatePeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: StatusUpdateEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusUpdateEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusUpdateEventType},
					Result: Result{},
				},
			},
		},
		{
			name: "NamespaceResyncEvents From NamespaceControllerPeriod",
			builder: &PublishingGroupBuilder{
				NamespaceControllerPeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: NamespaceSyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: NamespaceSyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: NamespaceSyncEventType},
					Result: Result{},
				},
			},
		},
		{
			name: "RetryEvents From RetryBackoff",
			builder: &PublishingGroupBuilder{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   0, // no backoff, just 1s delay
					Steps:    3, // at least as many as we expect
				},
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: RetrySyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType},
					Result: Result{},
				},
			},
		},
		{
			name: "RetryEvents From RetryBackoff with max steps",
			builder: &PublishingGroupBuilder{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   2, // double the delay each time
					Steps:    3,
				},
			},
			steps: []time.Duration{
				time.Second,
				3 * time.Second, //+2
				7 * time.Second, //+4
				8 * time.Second, //+1
			},
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 3s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 7s
					Result: Result{},
				},
				// No more retry events until another event sets ResetRetryBackoff=true
			},
		},
		{
			name: "RetryEvents and SyncEvents with RetryBackoff shorter than SyncPeriod",
			builder: &PublishingGroupBuilder{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   2, // double the delay each time
					Steps:    3,
				},
				SyncPeriod: 10 * time.Second,
			},
			steps: []time.Duration{
				time.Second,      // 0+1
				3 * time.Second,  // 1+2
				7 * time.Second,  // 3+4
				10 * time.Second, // 0+10
				20 * time.Second, // 10+10
				21 * time.Second, // 20+1
				23 * time.Second, // 21+2
				27 * time.Second, // 23+4
			},
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 3s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 7s
					Result: Result{},
				},
				// No more retry events until another event sets ResetRetryBackoff=true
				{
					Event: Event{Type: SyncEventType}, // 10s
					Result: Result{
						ResetRetryBackoff: false,
					},
				},
				{
					Event: Event{Type: SyncEventType}, // 20s
					Result: Result{
						ResetRetryBackoff: true,
					},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 21s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 23s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 27s
					Result: Result{},
				},
				// No more retry events until another event sets ResetRetryBackoff=true
			},
		},
		{
			name: "RetryEvents and SyncEvents with RetryBackoff longer than SyncPeriod",
			builder: &PublishingGroupBuilder{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   2, // double the delay each time
					Steps:    5, // more steps makes the max backoff longer than SyncPeriod
				},
				SyncPeriod: 20 * time.Second,
			},
			steps: []time.Duration{
				time.Second,
				3 * time.Second,  // 1+2
				7 * time.Second,  // 3+4
				15 * time.Second, // 7+8
				20 * time.Second, // 0+20
				31 * time.Second, // 15+16
				40 * time.Second, // 20+20
				60 * time.Second, // 40+20
				80 * time.Second, // 60+20
				81 * time.Second, // 80+1
			},
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 3s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 7s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 15s
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 20s
					Result: Result{
						RunAttempted:      true,
						ResetRetryBackoff: false, // No source changes, ParseAndUpdate skipped
					},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 31s
					Result: Result{},
				},
				// No more retry events until another event sets ResetRetryBackoff=true
				{
					Event: Event{Type: SyncEventType}, // 40s
					Result: Result{
						RunAttempted:      true,
						ResetRetryBackoff: false, // No source changes, ParseAndUpdate skipped
					},
				},
				{
					Event: Event{Type: SyncEventType}, // 60s
					Result: Result{
						RunAttempted:      true,
						ResetRetryBackoff: false, // No source changes, ParseAndUpdate skipped
					},
				},
				{
					Event: Event{Type: SyncEventType}, // 80s
					Result: Result{
						RunAttempted:      true,
						ResetRetryBackoff: true, // Source changes detected, ParseAndUpdate succeeded
					},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 81s
					Result: Result{},
				},
				// more backoff until max steps...
			},
		},
		{
			name: "SyncEvents, StatusUpdateEventType, FullSyncEvents",
			builder: &PublishingGroupBuilder{
				SyncPeriod:         700 * time.Millisecond,
				StatusUpdatePeriod: 300 * time.Millisecond,
			},
			// Explicit steps to avoid race conditions that make validation difficult.
			steps: []time.Duration{
				300 * time.Millisecond,
				600 * time.Millisecond,
				700 * time.Millisecond,
				1000 * time.Millisecond,
				1300 * time.Millisecond,
				1400 * time.Millisecond,
				1700 * time.Millisecond,
				2000 * time.Millisecond,
				2100 * time.Millisecond,
				2400 * time.Millisecond,
				2700 * time.Millisecond,
				2800 * time.Millisecond,
			},
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: StatusUpdateEventType}, // 300ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusUpdateEventType}, // 600ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 700ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusUpdateEventType}, // 1000ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusUpdateEventType}, // 1300ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 1400ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusUpdateEventType}, // 1700ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusUpdateEventType}, // 2000ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 2100ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusUpdateEventType}, // 2400ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusUpdateEventType}, // 2700ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 2800ms
					Result: Result{
						RunAttempted: true,
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			fakeClock := testingclock.NewFakeClock(time.Now().Truncate(time.Second))
			tc.builder.Clock = fakeClock

			var stepFunc func()
			if len(tc.steps) > 0 {
				// Explicit list of times
				startTime := fakeClock.Now()
				stepIndex := 0
				stepFunc = func() {
					if fakeClock.HasWaiters() {
						offset := tc.steps[stepIndex]
						t.Logf("Step %d", offset/1000/1000) // Nanos -> Millis
						fakeClock.SetTime(startTime.Add(offset))
						stepIndex++
					}
				}
			} else {
				// Fixed step size
				stepFunc = func() {
					if fakeClock.HasWaiters() {
						t.Log("Step")
						fakeClock.Step(tc.stepSize)
					}
				}
			}

			subscriber := &testSubscriber{
				T:              t,
				ExpectedEvents: tc.expectedEvents,
				StepFunc:       stepFunc,
				DoneFunc:       cancel,
			}
			funnel := &Funnel{
				Publishers: tc.builder.Build(),
				Subscriber: subscriber,
			}
			doneCh := funnel.Start(ctx)

			// Move clock forward, without blocking handler
			go stepFunc()

			// Wait until done
			<-doneCh

			var expectedEvents []Event
			for _, e := range tc.expectedEvents {
				expectedEvents = append(expectedEvents, e.Event)
			}

			testutil.AssertEqual(t, expectedEvents, subscriber.ReceivedEvents)
		})
	}
}

type eventResult struct {
	Event  Event
	Result Result
}

type testSubscriber struct {
	T              *testing.T
	ExpectedEvents []eventResult
	ReceivedEvents []Event
	StepFunc       func()
	DoneFunc       context.CancelFunc
}

func (s *testSubscriber) Handle(e Event) Result {
	s.T.Logf("Received event: %#v", e)
	// Record received events
	s.ReceivedEvents = append(s.ReceivedEvents, e)
	eventIndex := len(s.ReceivedEvents) - 1
	// Handle unexpected extra events
	if eventIndex >= len(s.ExpectedEvents) {
		s.T.Errorf("Unexpected event %d: %#v", eventIndex, e)
		return Result{}
	}
	// Handle expected events
	result := s.ExpectedEvents[eventIndex]
	eventIndex++
	// If all expected events have been seen, stop the funnel
	if eventIndex >= len(s.ExpectedEvents) {
		s.T.Log("Stopping: Received all expected events")
		s.DoneFunc()
	} else {
		// Move clock forward, without blocking return
		go s.StepFunc()
	}
	return result.Result
}
