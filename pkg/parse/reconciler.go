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

// Reconciler reconciles the cluster with config from the source of truth.
// TODO: Move to reconciler package; requires unwinding dependency cycles
type Reconciler interface {
	// Options returns the ReconcilerOptions used by this reconciler.
	Options() *ReconcilerOptions

	// ReconcilerState returns the current state of the parser/reconciler.
	ReconcilerState() *ReconcilerState

	// Reconcile the cluster with the source config.
	//
	// Reconcile has multiple phases:
	// - Fetch - Checks the shared filesystem for new source commits fetched by
	//     one of the *-sync sidecars.
	// - Render - Checks the shared filesystem for source rendered by the
	//     hydration-controller sidecar using helm or kustomize, if required.
	// - Read - Reads the fetch and render status from the shared filesystem and
	//     lists the source config files.
	// - Parse - Parses resource objects from the source config files, validates
	//    them, and adds custom metadata.
	// - Update (aka Sync) - Updates the cluster and remediator to reflect the
	//     latest resource object manifests in the source.
	Reconcile(ctx context.Context, trigger string) ReconcileResult

	// UpdateSyncStatus updates the RSync status to reflect asynchronous status
	// changes made by the remediator between Reconcile calls.
	// Returns an error if the status update failed or was cancelled.
	UpdateSyncStatus(context.Context) error
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

// ReconcilerState returns the current state of the reconciler.
func (p *reconciler) ReconcilerState() *ReconcilerState {
	return p.reconcilerState
}

// NewRootSyncReconciler creates a new reconciler for reconciling cluster-scoped
// and namespace-scoped resources configured with a RootSync.
func NewRootSyncReconciler(recOpts *ReconcilerOptions, parseOpts *RootOptions) Reconciler {
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

// NewRepoSyncReconciler creates a new reconciler for reconciling
// namespace-scoped resources configured with a RepoSync.
func NewRepoSyncReconciler(recOpts *ReconcilerOptions, parseOpts *Options) Reconciler {
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
