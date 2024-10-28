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

package parse

import (
	"context"
)

// SyncStatusClient provides methods to read and write RSync object status.
type SyncStatusClient interface {
	// ReconcilerStatusFromCluster reads the status of the reconciler from the RSync status.
	ReconcilerStatusFromCluster(ctx context.Context) (*ReconcilerStatus, error)
	// SetSourceStatus sets the source status and syncing condition on the RSync.
	SetSourceStatus(ctx context.Context, newStatus *SourceStatus) error
	// SetRenderingStatus sets the rendering status and syncing condition on the RSync.
	SetRenderingStatus(ctx context.Context, oldStatus, newStatus *RenderingStatus) error
	// SetSyncStatus sets the sync status and syncing condition on the RSync.
	SetSyncStatus(ctx context.Context, newStatus *SyncStatus) error
	// SetRequiresRendering sets the requires-rendering annotation on the RSync.
	SetRequiresRendering(ctx context.Context, renderingRequired bool) error
	// SetSourceAnnotations sets the source annotations on the RSync.
	SetSourceAnnotations(ctx context.Context, commit string) error
}
