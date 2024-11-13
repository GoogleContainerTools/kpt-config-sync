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

package reconciler

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/parse"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncclient"
	"kpt.dev/configsync/pkg/util"
	webhookconfiguration "kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Now, this is a story all about how
// This code got flipped-turned upside down
// And I'd like to take a minute
// Just sit right there
// I'll tell you how I changed all the trigger variable names without changing their string values...
//
// It turns out, the trigger string values are used as a ParserDuration metric label.
// So we changed the variable names to make sense, without breaking customer metrics.
const (
	triggerFullSync           = "resync"
	triggerSync               = "reimport"
	triggerRetry              = "retry"
	triggerManagementConflict = "managementConflict"
	triggerWatchUpdate        = "watchUpdate"
	triggerNamespaceUpdate    = "namespaceEvent"
)

const (
	// RenderingInProgress means that the configs are still being rendered by Config Sync.
	RenderingInProgress string = "Rendering is still in progress"

	// RenderingSucceeded means that the configs have been rendered successfully.
	RenderingSucceeded string = "Rendering succeeded"

	// RenderingFailed means that the configs have failed to be rendered.
	RenderingFailed string = "Rendering failed"

	// RenderingSkipped means that the configs don't need to be rendered.
	RenderingSkipped string = "Rendering skipped"

	// RenderingRequired means that the configs require rendering but the
	// hydration-controller is not currently running.
	RenderingRequired string = "Rendering required but is currently disabled"
	// RenderingNotRequired means that the configs do not require rendering but the
	// hydration-controller is currently running.
	RenderingNotRequired string = "Rendering not required but is currently enabled"
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
	syncStatusClient syncclient.SyncStatusClient
	parser           parse.Parser
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
func NewRootSyncReconciler(recOpts *ReconcilerOptions, parseOpts *syncclient.RootOptions) Reconciler {
	return &reconciler{
		options: recOpts,
		reconcilerState: &ReconcilerState{
			syncErrorCache: recOpts.SyncErrorCache,
		},
		syncStatusClient: &syncclient.RootSyncStatusClient{
			Options: parseOpts.Options,
		},
		parser: &parse.RootSyncParser{
			Options: parseOpts,
		},
	}
}

// NewRepoSyncReconciler creates a new reconciler for reconciling
// namespace-scoped resources configured with a RepoSync.
func NewRepoSyncReconciler(recOpts *ReconcilerOptions, parseOpts *syncclient.Options) Reconciler {
	return &reconciler{
		options: recOpts,
		reconcilerState: &ReconcilerState{
			syncErrorCache: recOpts.SyncErrorCache,
		},
		syncStatusClient: &syncclient.RepoSyncStatusClient{
			Options: parseOpts,
		},
		parser: &parse.RepoSyncParser{
			Options: parseOpts,
		},
	}
}

// ReconcileResult encapsulates the result of a reconciler.Reconcile.
// This simply allows explicitly naming return values in a way that makes the
// implementation easier to read.
type ReconcileResult struct {
	SourceChanged bool
	Success       bool
}

// Reconcile the cluster with the source config.
//
// Reconcile has multiple phases:
//   - Fetch - Checks the shared filesystem for new source commits fetched by
//     one of the *-sync sidecars.
//   - Render - Checks the shared filesystem for source rendered by the
//     hydration-controller sidecar using helm or kustomize, if required.
//   - Read - Reads the fetch and render status from the shared filesystem and
//     lists the source config files.
//   - Parse - Parses resource objects from the source config files, validates
//     them, and adds custom metadata.
//   - Update (aka Sync) - Updates the cluster and remediator to reflect the
//     latest resource object manifests in the source.
func (r *reconciler) Reconcile(ctx context.Context, trigger string) ReconcileResult {
	klog.Infof("Starting sync attempt (trigger: %s)", trigger)
	opts := r.Options()
	result := ReconcileResult{}
	state := r.ReconcilerState()

	// Initialize ReconcilerStatus from RSync status
	if state.status == nil {
		reconcilerStatus, err := r.syncStatusClient.GetReconcilerStatus(ctx)
		if err != nil {
			state.RecordFailure(opts.Clock, err)
			return result
		}
		state.status = reconcilerStatus
	}

	switch trigger {
	case triggerFullSync, triggerManagementConflict, triggerNamespaceUpdate:
		// Force parsing and updating, but skip fetch, render, and read unless required.
		state.RecordFullSyncStart()
	}

	newSourceStatus, syncPath, errs := r.fetch(ctx)
	if errs != nil {
		state.RecordFailure(opts.Clock, errs)
		return result
	}

	if opts.RenderingEnabled {
		if errs := r.render(ctx, newSourceStatus); errs != nil {
			state.RecordFailure(opts.Clock, errs)
			return result
		}
	}

	// Init cached source
	if state.cache.Source == nil {
		state.cache.Source = &syncclient.SourceState{}
	}

	// rendering is done, starts to read the source or hydrated configs.
	oldSyncPath := state.cache.Source.SyncPath
	if errs := r.read(ctx, trigger, newSourceStatus, syncPath); errs != nil {
		state.RecordFailure(opts.Clock, errs)
		return result
	}

	newSyncPath := state.cache.Source.SyncPath

	if newSyncPath != oldSyncPath {
		// If the commit, branch, or sync dir changed and read succeeded,
		// trigger retries to start again, if stopped.
		result.SourceChanged = true
	}

	// Skip parse-apply-watch if the trigger is `triggerSync` (aka "reimport")
	// and there are no new source changes. The reasons are:
	//   * If a former parse-apply-watch sequence for syncPath succeeded, there is no need to run the sequence again;
	//   * If all the former parse-apply-watch sequences for syncPath failed, the next retry will call the sequence.
	if trigger == triggerSync && oldSyncPath == newSyncPath {
		return result
	}

	if errs := r.parseAndUpdate(ctx, trigger); errs != nil {
		state.RecordFailure(opts.Clock, errs)
		return result
	}

	// Only checkpoint the state after *everything* succeeded, including status update.
	state.RecordSyncSuccess(opts.Clock)
	result.Success = true
	return result
}

// fetch waits for the *-sync sidecars to fetch the source manifests to the
// shared source volume.
// Updates the RSync status (source status and syncing condition).
func (r *reconciler) fetch(ctx context.Context) (*syncclient.SourceStatus, cmpath.Absolute, status.MultiError) {
	opts := r.Options()
	state := r.ReconcilerState()
	var syncPath cmpath.Absolute
	newSourceStatus := &syncclient.SourceStatus{}

	// pull the source commit and directory with retries within 5 minutes.
	newSourceStatus.Commit, syncPath, newSourceStatus.Errs = hydrate.SourceCommitAndSyncPathWithRetry(
		util.SourceRetryBackoff, opts.SourceType, opts.SourceDir, opts.SyncDir, opts.ReconcilerName)

	// Add pre-sync annotations to the object.
	// If updating the object fails, it's likely due to a signature verification error
	// from the webhook. In this case, add the error as a source error.
	if newSourceStatus.Errs == nil {
		if err := r.syncStatusClient.SetImageToSyncAnnotation(ctx, newSourceStatus.Commit); err != nil {
			newSourceStatus.Errs = status.Append(newSourceStatus.Errs, err)
		}
	}

	// Generate source spec from Reconciler config
	newSourceStatus.Spec = sourceSpecFromFileSource(opts.FileSource, opts.SourceType, newSourceStatus.Commit)

	// Only update the source status if there are errors or the commit changed.
	// Otherwise, parsing errors may be overwritten.
	// TODO: Decouple fetch & parse stages to use different status fields
	if newSourceStatus.Errs != nil || state.status.SourceStatus == nil || newSourceStatus.Commit != state.status.SourceStatus.Commit {
		newSourceStatus.LastUpdate = nowMeta(opts.Clock)
		if state.status.NeedToSetSourceStatus(newSourceStatus) {
			klog.V(3).Info("Updating source status (after fetch)")
			if statusErr := r.syncStatusClient.SetSourceStatus(ctx, newSourceStatus); statusErr != nil {
				return newSourceStatus, syncPath, status.Append(newSourceStatus.Errs, statusErr)
			}
			state.status.SourceStatus = newSourceStatus
		}
		// If there were fetch errors, stop, log them, and retry later
		if newSourceStatus.Errs != nil {
			return newSourceStatus, syncPath, newSourceStatus.Errs
		}
	}

	// Fetch successful
	return newSourceStatus, syncPath, nil
}

// render waits for the hydration-controller sidecar to render the source
// manifests on the shared source volume.
// Updates the RSync status (rendering status and syncing condition).
func (r *reconciler) render(ctx context.Context, sourceStatus *syncclient.SourceStatus) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()
	newRenderStatus := &syncclient.RenderingStatus{
		Spec:   sourceStatus.Spec,
		Commit: sourceStatus.Commit,
	}
	if state.status.RenderingStatus != nil {
		newRenderStatus.RequiresRendering = state.status.RenderingStatus.RequiresRendering
	}
	// Check the done file, created by the hydration-controller.
	// It should contain the last rendered commit.
	// RenderedCommit returns the empty string if the done file doesn't exist yet.
	doneFilePath := opts.RepoRoot.Join(cmpath.RelativeSlash(hydrate.DoneFile)).OSPath()
	renderedCommit, err := hydrate.RenderedCommit(doneFilePath)
	if err != nil {
		newRenderStatus.Message = RenderingFailed
		newRenderStatus.LastUpdate = nowMeta(opts.Clock)
		newRenderStatus.Errs = status.InternalHydrationError(err, RenderingFailed)
		klog.V(3).Info("Updating rendering status (before parse)")
		if statusErr := r.syncStatusClient.SetRenderingStatus(ctx, state.status.RenderingStatus, newRenderStatus); statusErr != nil {
			return status.Append(newRenderStatus.Errs, statusErr)
		}
		state.status.RenderingStatus = newRenderStatus
		return newRenderStatus.Errs
	}
	if renderedCommit == "" || renderedCommit != sourceStatus.Commit {
		newRenderStatus.Message = RenderingInProgress
		newRenderStatus.LastUpdate = nowMeta(opts.Clock)
		klog.V(3).Info("Updating rendering status (before parse)")
		if statusErr := r.syncStatusClient.SetRenderingStatus(ctx, state.status.RenderingStatus, newRenderStatus); statusErr != nil {
			return statusErr
		}
		state.status.RenderingStatus = newRenderStatus
		state.RecordRenderInProgress()
		// Transient error will be logged as info, instead of error,
		// but still trigger retry with backoff.
		return status.TransientError(errors.New(RenderingInProgress))
	}

	// Render successful
	return nil
}

// read source manifests from the shared source volume.
// Waits for rendering, if enabled.
// Updates the RSync status (source, rendering, and syncing condition).
func (r *reconciler) read(ctx context.Context, trigger string, sourceStatus *syncclient.SourceStatus, syncPath cmpath.Absolute) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()
	sourceState := &syncclient.SourceState{
		Spec:     sourceStatus.Spec,
		Commit:   sourceStatus.Commit,
		SyncPath: syncPath,
	}
	newRenderStatus, newSourceStatus := r.readFromSource(ctx, trigger, sourceState)
	if opts.RenderingEnabled != newRenderStatus.RequiresRendering {
		// the reconciler is misconfigured. set the annotation so that the reconciler-manager
		// will recreate this reconciler with the correct configuration.
		if err := r.syncStatusClient.SetRequiresRenderingAnnotation(ctx, newRenderStatus.RequiresRendering); err != nil {
			newRenderStatus.Errs = status.Append(newRenderStatus.Errs, err)
		}
	}
	newRenderStatus.LastUpdate = nowMeta(opts.Clock)
	// update the rendering status before source status because the parser needs to
	// read and parse the configs after rendering is done and there might have errors.
	klog.V(3).Info("Updating rendering status (after parse)")
	if statusErr := r.syncStatusClient.SetRenderingStatus(ctx, state.status.RenderingStatus, newRenderStatus); statusErr != nil {
		// Return both errors
		return status.Append(newRenderStatus.Errs, statusErr)
	}
	state.status.RenderingStatus = newRenderStatus
	if newRenderStatus.Errs != nil {
		return newRenderStatus.Errs
	}

	// Only call `SetSourceStatus` if `readFromSource` failed.
	// If `readFromSource` succeeds, `parse` may still fail,
	// and we don't want the source status flapping back and forth.
	if newSourceStatus.Errs != nil {
		newSourceStatus.LastUpdate = nowMeta(opts.Clock)
		if state.status.NeedToSetSourceStatus(newSourceStatus) {
			klog.V(3).Info("Updating source status (after parse)")
			if statusErr := r.syncStatusClient.SetSourceStatus(ctx, newSourceStatus); statusErr != nil {
				return status.Append(newSourceStatus.Errs, statusErr)
			}
			state.status.SourceStatus = newSourceStatus
		}
		return newSourceStatus.Errs
	}

	// Read successful
	return nil
}

// parseHydrationState reads from the file path which the hydration-controller
// container writes to. It checks if the hydrated files are ready and returns
// a renderingStatus.
func (r *reconciler) parseHydrationState(srcState *syncclient.SourceState, newRenderStatus *syncclient.RenderingStatus) (*syncclient.SourceState, *syncclient.RenderingStatus) {
	opts := r.Options()
	if !opts.RenderingEnabled {
		newRenderStatus.Message = RenderingSkipped
		return srcState, newRenderStatus
	}
	// Check if the hydratedRoot directory exists.
	// If exists, read the hydrated directory. Otherwise, fail.
	absHydratedRoot, err := cmpath.AbsoluteOS(opts.HydratedRoot)
	if err != nil {
		newRenderStatus.Message = RenderingFailed
		newRenderStatus.Errs = status.InternalHydrationError(err, "hydrated-dir must be an absolute path")
		return srcState, newRenderStatus
	}

	var hydrationErr hydrate.HydrationError
	if _, err := os.Stat(absHydratedRoot.OSPath()); err == nil {
		// pull the hydrated commit and directory with retries within 1 minute.
		srcState, hydrationErr = opts.ReadHydratedPathWithRetry(util.HydratedRetryBackoff, absHydratedRoot, opts.ReconcilerName, srcState)
		if hydrationErr != nil {
			newRenderStatus.Message = RenderingFailed
			newRenderStatus.Errs = status.HydrationError(hydrationErr.Code(), hydrationErr)
			return srcState, newRenderStatus
		}
		newRenderStatus.Message = RenderingSucceeded
	} else if !os.IsNotExist(err) {
		newRenderStatus.Message = RenderingFailed
		newRenderStatus.Errs = status.InternalHydrationError(err, "unable to evaluate the hydrated path %s", absHydratedRoot.OSPath())
		return srcState, newRenderStatus
	} else {
		// Source of truth does not require hydration, but hydration-controller is running
		newRenderStatus.Message = RenderingNotRequired
		newRenderStatus.RequiresRendering = false
		err := hydrate.NewTransientError(fmt.Errorf("sync source contains only wet configs and hydration-controller is running"))
		newRenderStatus.Errs = status.HydrationError(err.Code(), err)
		return srcState, newRenderStatus
	}
	return srcState, newRenderStatus
}

// readFromSource reads the source or hydrated configs, checks whether the SourceState in
// the cache is up-to-date. If the cache is not up-to-date, reads all the source or hydrated files.
// readFromSource returns the rendering status and source status.
func (r *reconciler) readFromSource(ctx context.Context, trigger string, srcState *syncclient.SourceState) (*syncclient.RenderingStatus, *syncclient.SourceStatus) {
	opts := r.Options()
	recState := r.ReconcilerState()
	start := opts.Clock.Now()

	newRenderStatus := &syncclient.RenderingStatus{
		Spec:              srcState.Spec,
		Commit:            srcState.Commit,
		RequiresRendering: opts.RenderingEnabled,
	}
	newSourceStatus := &syncclient.SourceStatus{
		Spec:   srcState.Spec,
		Commit: srcState.Commit,
	}

	srcState, newRenderStatus = r.parseHydrationState(srcState, newRenderStatus)
	if newRenderStatus.Errs != nil {
		return newRenderStatus, newSourceStatus
	}

	if srcState.SyncPath == recState.cache.Source.SyncPath {
		klog.V(4).Infof("Reconciler skipping listing source files; sync path unchanged: %s", srcState.SyncPath.OSPath())
		return newRenderStatus, newSourceStatus
	}
	klog.Infof("Reconciler listing source files from new sync path: %s", srcState.SyncPath.OSPath())

	// Read all the files under srcState.syncPath
	newSourceStatus.Errs = opts.ReadConfigFiles(srcState)

	if !opts.RenderingEnabled {
		// Check if any kustomization Files exist
		for _, fi := range srcState.Files {
			if hydrate.HasKustomization(path.Base(fi.OSPath())) {
				// Source of truth requires hydration, but the hydration-controller is not running
				newRenderStatus.Message = RenderingRequired
				newRenderStatus.RequiresRendering = true
				err := hydrate.NewTransientError(fmt.Errorf("sync source contains dry configs and hydration-controller is not running"))
				newRenderStatus.Errs = status.HydrationError(err.Code(), err)
				return newRenderStatus, newSourceStatus
			}
		}
	}

	if newSourceStatus.Errs != nil {
		recState.RecordReadFailure()
	} else {
		recState.RecordReadSuccess(srcState)
	}
	metrics.RecordParserDuration(ctx, trigger, "read", metrics.StatusTagKey(newSourceStatus.Errs), start)
	return newRenderStatus, newSourceStatus
}

func (r *reconciler) parseSource(ctx context.Context, trigger string) status.MultiError {
	state := r.ReconcilerState()
	if state.cache.ParserResultUpToDate() {
		return nil
	}

	opts := r.Options()
	start := opts.Clock.Now()
	var sourceErrs status.MultiError

	objs, errs := r.parser.ParseSource(ctx, state.cache.Source)
	if !opts.WebhookEnabled {
		klog.V(3).Infof("Removing %s annotation as Admission Webhook is disabled", metadata.DeclaredFieldsKey)
		for _, obj := range objs {
			core.RemoveAnnotations(obj, metadata.DeclaredFieldsKey)
		}
	}
	sourceErrs = status.Append(sourceErrs, errs)
	metrics.RecordParserDuration(ctx, trigger, "parse", metrics.StatusTagKey(sourceErrs), start)
	state.cache.SetParserResult(objs, sourceErrs)

	if !status.HasBlockingErrors(sourceErrs) && opts.WebhookEnabled {
		err := webhookconfiguration.Update(ctx, opts.Client, opts.DiscoveryClient, objs,
			client.FieldOwner(configsync.FieldManager))
		if err != nil {
			// Don't block if updating the admission webhook fails.
			// Return an error instead if we remove the remediator as otherwise we
			// will simply never correct the type.
			// This should be treated as a warning once we have
			// that capability.
			klog.Errorf("Failed to update admission webhook: %v", err)
			// TODO: Handle case where multiple reconciler Pods try to
			//  create or update the Configuration simultaneously.
		}
	}

	return sourceErrs
}

// parseAndUpdate parses objects from the source manifests, validates them, and
// then syncs them to the cluster with the Updater.
func (r *reconciler) parseAndUpdate(ctx context.Context, trigger string) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()
	klog.V(3).Info("Parser starting...")
	sourceErrs := r.parseSource(ctx, trigger)
	klog.V(3).Info("Parser stopped")
	newSourceStatus := &syncclient.SourceStatus{
		Spec:       state.cache.Source.Spec,
		Commit:     state.cache.Source.Commit,
		Errs:       sourceErrs,
		LastUpdate: nowMeta(opts.Clock),
	}
	if state.status.NeedToSetSourceStatus(newSourceStatus) {
		klog.V(3).Info("Updating source status (after parse)")
		if statusErr := r.syncStatusClient.SetSourceStatus(ctx, newSourceStatus); statusErr != nil {
			return status.Append(sourceErrs, statusErr)
		}
		state.status.SourceStatus = newSourceStatus
	}

	if status.HasBlockingErrors(sourceErrs) {
		return sourceErrs
	}

	// Create a new context with its cancellation function.
	ctxForUpdateSyncStatus, cancel := context.WithCancel(context.Background())

	go r.updateSyncStatusPeriodically(ctxForUpdateSyncStatus)

	klog.V(3).Info("Updater starting...")
	start := opts.Clock.Now()
	updateErrs := opts.Update(ctx, &state.cache)
	metrics.RecordParserDuration(ctx, trigger, "update", metrics.StatusTagKey(updateErrs), start)
	klog.V(3).Info("Updater stopped")

	// This is to terminate `updateSyncStatusPeriodically`.
	cancel()
	// TODO: Wait for periodic updates to stop

	// SyncErrors include errors from both the Updater and Remediator
	klog.V(3).Info("Updating sync status (after sync)")
	syncErrs := r.ReconcilerState().SyncErrors()
	// Return all the errors from the Parser, Updater, and Remediator
	reconcileErrs := status.Append(sourceErrs, syncErrs)

	// Copy the spec and commit from the source status
	syncStatus := &syncclient.SyncStatus{
		Spec:       state.status.SourceStatus.Spec,
		Syncing:    false,
		Commit:     state.cache.Source.Commit,
		Errs:       syncErrs,
		LastUpdate: nowMeta(opts.Clock),
	}
	if statusErr := r.setSyncStatus(ctx, syncStatus); statusErr != nil {
		return status.Append(reconcileErrs, statusErr)
	}
	return reconcileErrs
}

// setSyncStatus updates `.status.sync` and the Syncing condition, if needed,
// as well as `state.SyncStatus` if the update is successful.
func (r *reconciler) setSyncStatus(ctx context.Context, newSyncStatus *syncclient.SyncStatus) error {
	state := r.ReconcilerState()
	// Update the RSync status, if necessary
	if state.status.NeedToSetSyncStatus(newSyncStatus) {
		if statusErr := r.syncStatusClient.SetSyncStatus(ctx, newSyncStatus); statusErr != nil {
			return statusErr
		}
		state.status.SyncStatus = newSyncStatus
	}

	// Report conflict errors to the remote manager, if it's a RootSync.
	opts := r.Options()
	if err := reportRootSyncConflicts(ctx, opts.Client, opts.ManagementConflicts()); err != nil {
		return fmt.Errorf("failed to report remote conflicts: %w", err)
	}
	return nil
}

// updateSyncStatusPeriodically update the sync status periodically until the
// cancellation function of the context is called.
func (r *reconciler) updateSyncStatusPeriodically(ctx context.Context) {
	opts := r.Options()
	state := r.ReconcilerState()
	klog.V(3).Info("Periodic sync status updates starting...")
	updatePeriod := opts.StatusUpdatePeriod
	updateTimer := opts.Clock.NewTimer(updatePeriod)
	defer updateTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			// ctx.Done() is closed when the cancellation function of the context is called.
			klog.V(3).Info("Periodic sync status updates stopped")
			return

		case <-updateTimer.C():
			klog.V(3).Info("Updating sync status (periodic while syncing)")
			// Copy the spec and commit from the source status
			syncStatus := &syncclient.SyncStatus{
				Spec:       state.status.SourceStatus.Spec,
				Syncing:    true,
				Commit:     state.cache.Source.Commit,
				Errs:       r.ReconcilerState().SyncErrors(),
				LastUpdate: nowMeta(opts.Clock),
			}
			if err := r.setSyncStatus(ctx, syncStatus); err != nil {
				klog.Warningf("failed to update sync status: %v", err)
			}

			updateTimer.Reset(updatePeriod) // Schedule status update attempt
		}
	}
}

// reportRootSyncConflicts reports conflicts to the RootSync that manages the
// conflicting resources.
func reportRootSyncConflicts(ctx context.Context, k8sClient client.Client, conflictErrs []status.ManagementConflictError) error {
	if len(conflictErrs) == 0 {
		return nil
	}
	conflictingManagerErrors := map[string][]status.ManagementConflictError{}
	for _, conflictError := range conflictErrs {
		conflictingManager := conflictError.CurrentManager()
		conflictingManagerErrors[conflictingManager] = append(conflictingManagerErrors[conflictingManager], conflictError)
	}

	for conflictingManager, conflictErrors := range conflictingManagerErrors {
		scope, name := declared.ManagerScopeAndName(conflictingManager)
		if scope == declared.RootScope {
			// RootSync applier uses PolicyAdoptAll.
			// So it may fight, if the webhook is disabled.
			// Report the conflict to the other RootSync to make it easier to detect.
			klog.Infof("Detected conflict with RootSync manager %q", conflictingManager)
			if err := syncclient.PrependRootSyncRemediatorStatus(ctx, k8sClient, name, conflictErrors); err != nil {
				return fmt.Errorf("failed to update RootSync %q to prepend remediator conflicts: %w", name, err)
			}
		} else {
			// RepoSync applier uses PolicyAdoptIfNoInventory.
			// So it won't fight, even if the webhook is disabled.
			klog.Infof("Detected conflict with RepoSync manager %q", conflictingManager)
		}
	}
	return nil
}

// UpdateSyncStatus updates the RSync status to reflect asynchronous status
// changes made by the remediator between Reconcile calls.
func (r *reconciler) UpdateSyncStatus(ctx context.Context) error {
	opts := r.Options()
	// Skip updates if the remediator is not running yet, paused, or watches haven't been updated yet.
	// This implies that this reconciler has successfully parsed, rendered, validated, and synced.
	if !opts.Remediator.Remediating() {
		return nil
	}
	klog.V(3).Info("Updating sync status (periodic while not syncing)")
	state := r.ReconcilerState()
	// Don't update the sync spec or commit, just the errors and status.
	syncStatus := &syncclient.SyncStatus{
		Spec:       state.status.SyncStatus.Spec,
		Syncing:    false,
		Commit:     state.status.SyncStatus.Commit,
		Errs:       state.SyncErrors(),
		LastUpdate: nowMeta(opts.Clock),
	}
	return r.setSyncStatus(ctx, syncStatus)
}

func nowMeta(c clock.Clock) metav1.Time {
	return metav1.Time{Time: c.Now()}
}

// sourceSpecFromFileSource builds a SourceSpec from the FileSource.
// The type of SourceSpec depends on the SourceType.
// Commit is only necessary for Helm sources, because the chart Version is
// parsed from the "commit" string (`chart:version`).
func sourceSpecFromFileSource(source syncclient.FileSource, sourceType configsync.SourceType, commit string) syncclient.SourceSpec {
	var ss syncclient.SourceSpec
	switch sourceType {
	case configsync.GitSource:
		ss = syncclient.GitSourceSpec{
			Repo:     source.SourceRepo,
			Revision: source.SourceRev,
			Branch:   source.SourceBranch,
			Dir:      source.SyncDir.SlashPath(),
		}
	case configsync.OciSource:
		ss = syncclient.OCISourceSpec{
			Image: source.SourceRepo,
			Dir:   source.SyncDir.SlashPath(),
		}
	case configsync.HelmSource:
		ss = syncclient.HelmSourceSpec{
			Repo:    source.SourceRepo,
			Chart:   source.SyncDir.SlashPath(),
			Version: getChartVersionFromCommit(source.SourceRev, commit),
		}
	}
	return ss
}

// sourceRev will display the source version,
// but that could potentially be provided to use as a range of
// versions from which we pick the latest. We should display the
// version that was actually pulled down if we can.
// commit is expected to be of the format `chart:version`,
// so we parse it to grab the version.
func getChartVersionFromCommit(sourceRev, commit string) string {
	split := strings.Split(commit, ":")
	if len(split) == 2 {
		return split[1]
	}
	return sourceRev
}
