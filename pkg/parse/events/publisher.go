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

// Publisher writes events to a channel and defines how to handle them.
type Publisher interface {
	// Type of events published by this Publisher.
	Type() EventType
	// Start the event publisher and return the event channel.
	// Messages will be sent to the channel with an optional typed payload.
	// Channel will be closed when the context is done.
	Start(context.Context) reflect.Value
	// Publish an event to a specific Subscriber, and return the Result.
	// Publish should be called when the event channel sends an event.
	Publish(Subscriber) Result
	// HandleResult handles the result of all types of events.
	// This allows a Publisher to handle signals from other Publishers.
	HandleResult(Result)
}

// Subscriber handles an Event and returns the result.
type Subscriber interface {
	// Handle an event and return the result.
	Handle(Event) Result
}

// Result encapsulates the result of a ConsumeFunc.
// This simply allows explicitly naming return values in a way that makes the
// implementation easier to read.
type Result struct {
	// RunAttempted triggers timer reset of any events using the
	// ResetOnRunAttemptPublisher.
	RunAttempted bool
	// ResetRetryBackoff triggers backoff reset for the RetrySyncPublisher.
	ResetRetryBackoff bool
	// TriggerRetryBackoff triggers backoff for the RetrySyncPublisher.
	// If it is false, the RetrySyncPublisher publishes events periodically, every
	// 1 second, to check if a retry needs to be triggered.
	TriggerRetryBackoff bool
}

// NewTimeDelayPublisher constructs an TimeDelayPublisher that generates and
// handles the specified events.
func NewTimeDelayPublisher(eventType EventType, c clock.Clock, period time.Duration) *TimeDelayPublisher {
	return &TimeDelayPublisher{
		EventType: eventType,
		Clock:     c,
		Period:    period,
	}
}

// TimeDelayPublisher sends events periodically using a timer that is reset after
// each event is handled. This avoids unhandled events stacking up waiting to be
// handled.
type TimeDelayPublisher struct {
	EventType EventType
	Clock     clock.Clock
	Period    time.Duration

	timer clock.Timer
}

// Type of events produced by this publisher.
func (s *TimeDelayPublisher) Type() EventType {
	return s.EventType
}

// Start the timer and return the event channel.
func (s *TimeDelayPublisher) Start(ctx context.Context) reflect.Value {
	s.timer = s.Clock.NewTimer(s.Period)
	go func() {
		<-ctx.Done()
		s.timer.Stop()
	}()
	return reflect.ValueOf(s.timer.C())
}

// Publish calls the HandleFunc with a new event and resets the delay timer.
func (s *TimeDelayPublisher) Publish(subscriber Subscriber) Result {
	result := subscriber.Handle(Event{Type: s.EventType})

	// Schedule next event
	s.timer.Reset(s.Period)
	return result
}

// HandleResult is a no-op.
func (s *TimeDelayPublisher) HandleResult(_ Result) {}

// NewRetrySyncPublisher constructs an RetrySyncPublisher that generates and
// handles RetrySyncEvents with retry backoff.
func NewRetrySyncPublisher(c clock.Clock, backoff wait.Backoff) *RetrySyncPublisher {
	return &RetrySyncPublisher{
		EventType: RetrySyncEventType,
		Clock:     c,
		Backoff:   backoff,
	}
}

// RetrySyncPublisher sends StatusEvents periodically using a backoff
// timer that is incremented after each event is handled.
//
// Unlike, TimeDelayPublisher, RetrySyncPublisher always increases the
// delay, unless the subscriber sets Result.ResetRetryBackoff.
type RetrySyncPublisher struct {
	EventType EventType
	Clock     clock.Clock
	Backoff   wait.Backoff

	currentBackoff wait.Backoff
	retryLimit     int
	timer          clock.Timer

	triggerRetryBackoff bool
}

// Type of events produced by this publisher.
func (s *RetrySyncPublisher) Type() EventType {
	return s.EventType
}

// Start the timer and return the event channel.
func (s *RetrySyncPublisher) Start(ctx context.Context) reflect.Value {
	s.currentBackoff = util.CopyBackoff(s.Backoff)
	s.retryLimit = s.currentBackoff.Steps
	s.timer = s.Clock.NewTimer(s.currentBackoff.Duration)
	go func() {
		<-ctx.Done()
		s.timer.Stop()
	}()
	return reflect.ValueOf(s.timer.C())
}

// Publish calls the HandleFunc with a new event, increments the backoff
// step, and updates the delay timer.
// If the maximum number of retries has been reached, the HandleFunc is NOT
// called and an empty Result is returned.
func (s *RetrySyncPublisher) Publish(subscriber Subscriber) Result {
	var retryDuration time.Duration
	if s.triggerRetryBackoff {
		if s.currentBackoff.Steps == 0 {
			klog.Infof("Retry limit (%v) has been reached", s.retryLimit)
			// Don't reset retryTimer if retry limit has been reached.
			return Result{}
		}

		retryDuration = s.currentBackoff.Step()
		retries := s.retryLimit - s.currentBackoff.Steps
		klog.Infof("a retry is triggered (retries: %v/%v)", retries, s.retryLimit)
	} else {
		retryDuration = time.Second
	}

	result := subscriber.Handle(Event{Type: s.EventType})

	// Schedule next event
	s.timer.Reset(retryDuration)
	return result
}

// HandleResult resets the backoff timer if ResetRetryBackoff is true.
func (s *RetrySyncPublisher) HandleResult(result Result) {
	if result.ResetRetryBackoff {
		s.currentBackoff = util.CopyBackoff(s.Backoff)
		s.timer.Reset(s.currentBackoff.Duration)
	}
	s.triggerRetryBackoff = result.TriggerRetryBackoff
}

// NewResetOnRunAttemptPublisher constructs an ResetOnRunPublisher that
// generates and handles the specified events and resets the delay any time
// Result.RunAttempted=true.
func NewResetOnRunAttemptPublisher(eventType EventType, c clock.Clock, period time.Duration) *ResetOnRunAttemptPublisher {
	return &ResetOnRunAttemptPublisher{
		TimeDelayPublisher: TimeDelayPublisher{
			EventType: eventType,
			Clock:     c,
			Period:    period,
		},
	}
}

// ResetOnRunAttemptPublisher is a TimeDelayPublisher that is automatically
// delayed when Result.RunAttempted is set by any event.
type ResetOnRunAttemptPublisher struct {
	TimeDelayPublisher
}

// HandleResult resets the delay timer if DelayStatusUpdate is true.
func (s *ResetOnRunAttemptPublisher) HandleResult(result Result) {
	s.TimeDelayPublisher.HandleResult(result)
	if result.RunAttempted {
		s.timer.Reset(s.Period)
	}
}
