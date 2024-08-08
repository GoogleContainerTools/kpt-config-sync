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
)

// ScheduleFunc generates a list of EventProducer, each with their own event
// generating channel and event handler function.
type ScheduleFunc func(context.Context) []EventProducer

// HandleEventFunc handles an Event and decides whether to call the RunFunc.
type HandleEventFunc func(Event) HandleResult

// HandleResult encapsulates the result of a HandleEventFunc.
// This simply allows explicitly naming return values in a way that makes the
// implementation easier to read.
type HandleResult struct {
	// DelayStatusUpdate tells GenerateFunc to reset the status update timer.
	DelayStatusUpdate bool
	// ResetRetryBackoff tells GenerateFunc to reset the retry backoff.
	ResetRetryBackoff bool
}

// Event represents the cause of HandleEventFunc being called.
// Some event types may have special fields or methods.
type Event interface {
	// Type of the event
	Type() string
}

// FullSyncEvent triggers a full resync, with an empty/reset cache.
type FullSyncEvent struct{}

// Type of the event
func (re FullSyncEvent) Type() string {
	return "FullSyncEvent"
}

// SyncEvent triggers a sync with the cache as-is.
type SyncEvent struct{}

// Type of the event
func (se SyncEvent) Type() string {
	return "SyncEvent"
}

// StatusEvent triggers an attempt to update the RSync status, if necessary.
type StatusEvent struct{}

// Type of the event
func (se StatusEvent) Type() string {
	return "StatusEvent"
}

// NamespaceResyncEvent triggers a full resync, with an empty cache, if the
// namespace-controller decides it is necessary.
//
// TODO: Replace this with a ResyncEvent triggered by the namespace-controller
// via a buffered channel.
type NamespaceResyncEvent struct{}

// Type of the event
func (nre NamespaceResyncEvent) Type() string {
	return "NamespaceResyncEvent"
}

// RetrySyncEvent is an event that triggers a retry.
//
// See SkipChan() for how to handle skipping a retry.
// See ResetRetryBackoff() for how to handle resetting the retry backoff.
type RetrySyncEvent struct{}

// Type of the event
func (re RetrySyncEvent) Type() string {
	return "RetryEvent"
}
