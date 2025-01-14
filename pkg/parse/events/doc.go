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

// Package events contains a set of Events sent by Publishers.
//
// PublishingGroupBuilder can be used to Build a set of Publishers.
//
// Publisher types:
// - TimeDelayPublisher         - Publishes events with a configurable time delay.
// - ResetOnRunAttemptPublisher - TimeDelayPublisher with resettable timer (Result.RunAttempted).
// - RetrySyncPublisher         - TimeDelayPublisher with resettable backoff (Result.ResetRetryBackoff).
//
// Events and their Publisher:
// - SyncEvent             - ResetOnRunAttemptPublisher (SyncPeriod)
// - NamespaceResyncEvent  - TimeDelayPublisher (NamespaceControllerPeriod)
// - RetrySyncEvent        - RetrySyncPublisher (RetryBackoff)
// - StatusEvent           - TimeDelayPublisher (StatusUpdatePeriod)
//
// EventResult flags:
// - ResetRetryBackoff - Set after a sync succeeds or the source changed (spec or commit).
// - DelayStatusUpdate - Set after a sync is attempted.
//
// PublishingGroupBuilder can be used to Build a set of the above Publishers.
package events
