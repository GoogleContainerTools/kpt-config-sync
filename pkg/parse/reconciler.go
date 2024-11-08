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

	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/status"
)

// Reconciler represents a parser that can be pointed at and continuously parse a source.
// TODO: Move to reconciler package; requires unwinding dependency cycles
type Reconciler interface {
	// Options returns the ReconcilerOptions used by this reconciler.
	Options() *ReconcilerOptions
	// SyncStatusClient returns the SyncStatusClient used by this reconciler.
	SyncStatusClient() SyncStatusClient
	// Parser returns the Parser used by this reconciler.
	Parser() Parser
	// ReconcilerState returns the current state of the parser/reconciler.
	ReconcilerState() *ReconcilerState

	// Render waits for the hydration-controller sidecar to render the source
	// manifests on the shared source volume.
	// Updates the RSync status (rendering status and syncing condition).
	//
	// Render is exposed for use by DefaultRunFunc.
	Render(context.Context, *SourceStatus) status.MultiError

	// Read source manifests from the shared source volume.
	// Waits for rendering, if enabled.
	// Updates the RSync status (source, rendering, and syncing condition).
	//
	// Read is exposed for use by DefaultRunFunc.
	Read(ctx context.Context, trigger string, sourceStatus *SourceStatus, syncPath cmpath.Absolute) status.MultiError

	// ParseAndUpdate parses objects from the source manifests, validates them,
	// and then syncs them to the cluster with the Updater.
	//
	// ParseAndUpdate is exposed for use by DefaultRunFunc.
	ParseAndUpdate(ctx context.Context, trigger string) status.MultiError

	// SetSyncStatus updates `.status.sync` and the Syncing condition, if needed,
	// as well as `state.syncStatus` and `state.syncingConditionLastUpdate` if
	// the update is successful.
	//
	// SetSyncStatus is exposed for use by the EventHandler, for periodic status
	// updates.
	SetSyncStatus(context.Context, *SyncStatus) error
}

// TODO: Move to reconciler package; requires unwinding dependency cycles
type reconciler struct {
	options          *ReconcilerOptions
	syncStatusClient SyncStatusClient
	parser           Parser
	reconcilerState  *ReconcilerState
}

var _ Reconciler = &reconciler{}

// Options returns the ReconcilerOptions used by this reconciler.
func (p *reconciler) Options() *ReconcilerOptions {
	return p.options
}

// SyncStatusClient returns the SyncStatusClient used by this reconciler.
func (p *reconciler) SyncStatusClient() SyncStatusClient {
	return p.syncStatusClient
}

// ReconcilerState returns the current state of the reconciler.
func (p *reconciler) Parser() Parser {
	return p.parser
}

// ReconcilerState returns the current state of the reconciler.
func (p *reconciler) ReconcilerState() *ReconcilerState {
	return p.reconcilerState
}

// NewRootRunner creates a new runnable parser for parsing a Root repository.
func NewRootRunner(recOpts *ReconcilerOptions, parseOpts *RootOptions) Reconciler {
	return &reconciler{
		options: recOpts,
		reconcilerState: &ReconcilerState{
			syncErrorCache: recOpts.SyncErrorCache,
		},
		syncStatusClient: &rootSyncStatusClient{
			options: parseOpts.Options,
		},
		parser: &rootSyncParser{
			options: parseOpts,
		},
	}
}

// NewNamespaceRunner creates a new runnable parser for parsing a Namespace repo.
func NewNamespaceRunner(recOpts *ReconcilerOptions, parseOpts *Options) Reconciler {
	return &reconciler{
		options: recOpts,
		reconcilerState: &ReconcilerState{
			syncErrorCache: recOpts.SyncErrorCache,
		},
		syncStatusClient: &repoSyncStatusClient{
			options: parseOpts,
		},
		parser: &repoSyncParser{
			options: parseOpts,
		},
	}
}
