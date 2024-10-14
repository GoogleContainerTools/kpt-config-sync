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
		name                   string
		builder                *PublishingGroupBuilder
		stepSize               time.Duration
		steps                  []time.Duration
		expectedRemainingSteps int
		expectedEvents         []eventResult
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
			name: "ResyncEvents From SyncWithReimportPeriod",
			builder: &PublishingGroupBuilder{
				SyncWithReimportPeriod: time.Second,
			},
			stepSize: time.Second,
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: SyncWithReimportEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: SyncWithReimportEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: SyncWithReimportEventType},
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
					Event:  Event{Type: StatusEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType},
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
					Event:  Event{Type: NamespaceResyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: NamespaceResyncEventType},
					Result: Result{},
				},
				{
					Event:  Event{Type: NamespaceResyncEventType},
					Result: Result{},
				},
			},
		},
		{
			name: "RetryEvents From RetryBackoff with TriggerRetryBackoff on and 0 factor",
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
					Event: Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 2s, backoff enabled, but factor is 0, so just 1s delay
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 3s backoff enabled, but factor is 0, so just 1s delay
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 4s backoff enabled, but factor is 0, so just 1s delay
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				// No more retry events until another event sets ResetRetryBackoff=true
			},
			expectedRemainingSteps: 0, // reach the retry limit
		},
		{
			name: "RetryEvents From RetryBackoff with TriggerRetryBackoff off and 0 factor",
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
					Event:  Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{},
				},
			},
			expectedRemainingSteps: 3, // backoff disabled, so remaining steps not changed.
		},
		{
			name: "RetryEvents From RetryBackoff with TriggerRetryBackoff on and non-zero factor",
			builder: &PublishingGroupBuilder{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   2, // double the delay each time
					Steps:    3,
				},
			},
			steps: []time.Duration{
				time.Second,
				2 * time.Second,  //+1
				3 * time.Second,  //+1
				5 * time.Second,  //+2
				9 * time.Second,  //+4
				10 * time.Second, //+1
			},
			expectedEvents: []eventResult{
				{
					Event: Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 2s, backoff enabled, retry duration 1s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 3s, backoff enabled, retry duration 2s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 5s, backoff enabled, retry duration 4s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				// No more retry events until another event sets ResetRetryBackoff=true
			},
			expectedRemainingSteps: 0, // reach the retry limit
		},
		{
			name: "RetryEvents From RetryBackoff with TriggerRetryBackoff off and non-zero factor",
			builder: &PublishingGroupBuilder{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   2, // double the delay each time
					Steps:    3,
				},
			},
			steps: []time.Duration{
				time.Second,
				2 * time.Second,  //+1
				3 * time.Second,  //+1
				5 * time.Second,  //+2
				9 * time.Second,  //+4
				10 * time.Second, //+1
			},
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: RetrySyncEventType}, // 1s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 2s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 3s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 5s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 9s, backoff disabled, retry duration 1s
					Result: Result{},
				},
				{
					Event:  Event{Type: RetrySyncEventType}, // 10s, backoff disabled, retry duration 1s
					Result: Result{},
				},
			},
			expectedRemainingSteps: 3, // backoff disabled, so remaining steps not changed.
		},
		{
			name: "RetryEvents From RetryBackoff with ResetRetryBackoff",
			builder: &PublishingGroupBuilder{
				RetryBackoff: wait.Backoff{
					Duration: time.Second,
					Factor:   2, // double the delay each time
					Steps:    3,
				},
				SyncPeriod: 10 * time.Second,
			},
			steps: []time.Duration{
				time.Second,
				2 * time.Second,  //+1
				3 * time.Second,  //+2
				5 * time.Second,  //+4
				10 * time.Second, //+10 for sync event
				11 * time.Second, //+1
				12 * time.Second, //+1
			},
			expectedEvents: []eventResult{
				{
					Event: Event{Type: RetrySyncEventType}, // 1s, backoff disabled and retry duration 1s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 2s, backoff enabled and retry duration 1s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 3s backoff enabled and retry duration 2s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 5s backoff enabled and retry duration 4s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: SyncEventType}, // 10s backoff enabled and retry duration 1s
					Result: Result{
						ResetRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 11s backoff disabled and retry duration 1s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
				{
					Event: Event{Type: RetrySyncEventType}, // 12s backoff enabled and retry duration 1s
					Result: Result{
						TriggerRetryBackoff: true,
					},
				},
			},
			expectedRemainingSteps: 2,
		},
		{
			name: "SyncEvents, StatusEventType, ResyncEvents",
			builder: &PublishingGroupBuilder{
				SyncPeriod:             700 * time.Millisecond,
				SyncWithReimportPeriod: 4000 * time.Millisecond,
				StatusUpdatePeriod:     300 * time.Millisecond,
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
				3100 * time.Millisecond,
				3400 * time.Millisecond,
				3500 * time.Millisecond,
				3800 * time.Millisecond,
				4000 * time.Millisecond,
				4300 * time.Millisecond,
				4600 * time.Millisecond,
				4700 * time.Millisecond,
				5000 * time.Millisecond,
				5300 * time.Millisecond,
				5400 * time.Millisecond,
				5700 * time.Millisecond,
				6000 * time.Millisecond,
				6100 * time.Millisecond,
				6400 * time.Millisecond,
				6700 * time.Millisecond,
				6800 * time.Millisecond,
				7100 * time.Millisecond,
				7400 * time.Millisecond,
				7500 * time.Millisecond,
				7800 * time.Millisecond,
				8000 * time.Millisecond,
				8300 * time.Millisecond,
				8600 * time.Millisecond,
				8700 * time.Millisecond,
			},
			expectedEvents: []eventResult{
				{
					Event:  Event{Type: StatusEventType}, // 300ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 600ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 700ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 1000ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 1300ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 1400ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 1700ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 2000ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 2100ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 2400ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 2700ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 2800ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 3100ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 3400ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 3500ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 3800ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncWithReimportEventType}, // 4000ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 4300ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 4600ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 4700ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 5000ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 5300ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 5400ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 5700ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 6000ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 6100ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 6400ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 6700ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 6800ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 7100ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 7400ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 7500ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 7800ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncWithReimportEventType}, // 8000ms
					Result: Result{
						RunAttempted: true,
					},
				},
				{
					Event:  Event{Type: StatusEventType}, // 8300ms
					Result: Result{},
				},
				{
					Event:  Event{Type: StatusEventType}, // 8600ms
					Result: Result{},
				},
				{
					Event: Event{Type: SyncEventType}, // 8700ms
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

			publishers := tc.builder.Build()
			subscriber := &testSubscriber{
				T:              t,
				ExpectedEvents: tc.expectedEvents,
				StepFunc:       stepFunc,
				DoneFunc:       cancel,
			}
			funnel := &Funnel{
				Publishers: publishers,
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

			for _, publisher := range publishers {
				if publisher.Type() == RetrySyncEventType {
					retryPublisher := publisher.(*RetrySyncPublisher)
					testutil.AssertEqual(t, tc.expectedRemainingSteps, retryPublisher.currentBackoff.Steps)
				}
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
