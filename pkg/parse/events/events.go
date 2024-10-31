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

// Event represents the cause of HandleEventFunc being called.
// Some event types may have special fields or methods.
type Event struct {
	// Type of the event
	Type EventType
}

// EventType is the name of the event
type EventType string

const (
	// FullSyncEventType is the EventType for a sync from disk,
	// including reading and parsing resource objects from the shared volume.
	FullSyncEventType EventType = "FullSyncEvent"
	// SyncEventType is the EventType for a sync from the source cache,
	// only parsing objects from the shared volume if there's a new commit.
	SyncEventType EventType = "SyncEvent"
	// StatusUpdateEventType is the EventType for a periodic RSync status update.
	StatusUpdateEventType EventType = "StatusUpdateEvent"
	// NamespaceSyncEventType is the EventType for a sync triggered by an
	// update to a selected namespace.
	NamespaceSyncEventType EventType = "NamespaceSyncEvent"
	// RetrySyncEventType is the EventType for a sync triggered by an error
	// during a previous sync attempt.
	RetrySyncEventType EventType = "RetrySyncEvent"
)
