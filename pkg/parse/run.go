// Copyright 2022 Google LLC
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
	"errors"
	"fmt"
	"os"
	"path"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/status"
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
	result := ReconcileResult{}
	opts := r.Options()
	state := r.ReconcilerState()
	startTime := nowMeta(opts.Clock)

	// Initialize ReconcilerStatus from RSync status
	if state.status == nil {
		klog.V(3).Infof("Initializing reconciler status from %s status", opts.Options.Scope.SyncKind())
		reconcilerStatus, err := r.syncStatusClient.GetReconcilerStatus(ctx)
		if err != nil {
			state.RecordFailure(opts.Clock, err)
			return result
		}
		state.status = reconcilerStatus
	}

	// Perform full-sync, if required
	if trigger == triggerSync && state.IsFullSyncRequired(startTime, opts.FullSyncPeriod) {
		trigger = triggerFullSync
	}

	klog.Infof("Starting sync attempt (trigger: %s)", trigger)

	switch trigger {
	case triggerFullSync, triggerManagementConflict, triggerNamespaceUpdate:
		// Force parsing and updating, but skip fetch, render, and read unless required.
		state.RecordFullSyncStart(startTime)
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
	if state.cache.source == nil {
		state.cache.source = &sourceState{}
	}

	// rendering is done, starts to read the source or hydrated configs.
	oldSyncPath := state.cache.source.syncPath
	if errs := r.read(ctx, trigger, newSourceStatus, syncPath); errs != nil {
		state.RecordFailure(opts.Clock, errs)
		return result
	}

	newSyncPath := state.cache.source.syncPath

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

	parseErrs := r.parse(ctx, trigger)
	// Fail if there are any blocking errors.
	// Otherwise, continue to sync objects with known scope.
	if status.HasBlockingErrors(parseErrs) {
		state.RecordFailure(opts.Clock, parseErrs)
		return result
	}

	if opts.WebhookEnabled {
		err := webhookconfiguration.Update(ctx, opts.Client, opts.DiscoveryClient,
			state.cache.parse.GKVs(), client.FieldOwner(configsync.FieldManager))
		if err != nil {
			// RBAC needs to be set up manually by the user to allow updating the webhook.
			// TODO: Only continue if it's an authorization error, others should trigger retry
			klog.Errorf("Failed to update admission webhook: %v", err)
		}
	}

	updateErrs := r.update(ctx, trigger)
	// Fail if there are any update errors or non-blocking parse errors.
	if parseErrs != nil || updateErrs != nil {
		state.RecordFailure(opts.Clock, status.Append(parseErrs, updateErrs))
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
func (r *reconciler) fetch(ctx context.Context) (*SourceStatus, cmpath.Absolute, status.MultiError) {
	opts := r.Options()
	state := r.ReconcilerState()
	var syncPath cmpath.Absolute
	newSourceStatus := &SourceStatus{}

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
	newSourceStatus.Spec = SourceSpecFromFileSource(opts.FileSource, opts.SourceType, newSourceStatus.Commit)

	// Only update the source status if there are errors or the commit changed.
	// Otherwise, parsing errors may be overwritten.
	// TODO: Decouple fetch & parse stages to use different status fields
	if newSourceStatus.Errs != nil || state.status.SourceStatus == nil || newSourceStatus.Commit != state.status.SourceStatus.Commit {
		newSourceStatus.LastUpdate = nowMeta(opts.Clock)
		if state.status.needToSetSourceStatus(newSourceStatus) {
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
func (r *reconciler) render(ctx context.Context, sourceStatus *SourceStatus) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()
	newRenderStatus := &RenderingStatus{
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
		klog.V(3).Info("Updating rendering status (before read)")
		if statusErr := r.syncStatusClient.SetRenderingStatus(ctx, state.status.RenderingStatus, newRenderStatus); statusErr != nil {
			return status.Append(newRenderStatus.Errs, statusErr)
		}
		state.status.RenderingStatus = newRenderStatus
		return newRenderStatus.Errs
	}
	if renderedCommit == "" || renderedCommit != sourceStatus.Commit {
		newRenderStatus.Message = RenderingInProgress
		newRenderStatus.LastUpdate = nowMeta(opts.Clock)
		klog.V(3).Info("Updating rendering status (before read)")
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
func (r *reconciler) read(ctx context.Context, trigger string, sourceStatus *SourceStatus, syncPath cmpath.Absolute) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()
	sourceState := &sourceState{
		spec:     sourceStatus.Spec,
		commit:   sourceStatus.Commit,
		syncPath: syncPath,
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
	klog.V(3).Info("Updating rendering status (after read)")
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
		if state.status.needToSetSourceStatus(newSourceStatus) {
			klog.V(3).Info("Updating source status (after read)")
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
func (r *reconciler) parseHydrationState(srcState *sourceState, newRenderStatus *RenderingStatus) (*sourceState, *RenderingStatus) {
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
		srcState, hydrationErr = opts.readHydratedPathWithRetry(util.HydratedRetryBackoff, absHydratedRoot, opts.ReconcilerName, srcState)
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

// readFromSource reads the source or hydrated configs, checks whether the sourceState in
// the cache is up-to-date. If the cache is not up-to-date, reads all the source or hydrated files.
// readFromSource returns the rendering status and source status.
func (r *reconciler) readFromSource(ctx context.Context, trigger string, srcState *sourceState) (*RenderingStatus, *SourceStatus) {
	opts := r.Options()
	recState := r.ReconcilerState()
	start := opts.Clock.Now()

	newRenderStatus := &RenderingStatus{
		Spec:              srcState.spec,
		Commit:            srcState.commit,
		RequiresRendering: opts.RenderingEnabled,
	}
	newSourceStatus := &SourceStatus{
		Spec:   srcState.spec,
		Commit: srcState.commit,
	}

	srcState, newRenderStatus = r.parseHydrationState(srcState, newRenderStatus)
	if newRenderStatus.Errs != nil {
		return newRenderStatus, newSourceStatus
	}

	if srcState.syncPath == recState.cache.source.syncPath {
		klog.V(4).Infof("Reconciler skipping listing source files; sync path unchanged: %s", srcState.syncPath.OSPath())
		return newRenderStatus, newSourceStatus
	}
	klog.Infof("Reconciler listing source files from new sync path: %s", srcState.syncPath.OSPath())

	// Read all the files under srcState.syncPath
	newSourceStatus.Errs = opts.readConfigFiles(srcState)

	if !opts.RenderingEnabled {
		// Check if any kustomization Files exist
		for _, fi := range srcState.files {
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

// parse objects from the source files and perform scope validation.
// If any objects have unknown scope, parse will return non-blocking errors.
func (r *reconciler) parse(ctx context.Context, trigger string) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()

	var parseErrs status.MultiError
	if state.cache.parse.IsUpdateRequired() {
		klog.V(3).Info("Parsing starting...")
		start := opts.Clock.Now()
		var objs []ast.FileObject
		objs, parseErrs = r.parser.ParseSource(ctx, state.cache.source)
		if !opts.WebhookEnabled {
			klog.V(3).Infof("Removing %s annotation as Admission Webhook is disabled", metadata.DeclaredFieldsKey)
			for _, obj := range objs {
				core.RemoveAnnotations(obj, metadata.DeclaredFieldsKey)
			}
		}
		metrics.RecordParserDuration(ctx, trigger, "parse", metrics.StatusTagKey(parseErrs), start)
		state.cache.UpdateParseResult(objs, parseErrs, nowMeta(opts.Clock))
		klog.V(3).Info("Parsing stopped")
	} else {
		klog.V(3).Info("Parsing skipped")
	}

	// Update the source status if the status has changed, whether there are any
	// source errors or not. This confirms whether the fetch & parse stages
	// succeeded, since they share the same RSync `status.source` fields.
	newSourceStatus := &SourceStatus{
		Spec:       state.cache.source.spec,
		Commit:     state.cache.source.commit,
		Errs:       parseErrs,
		LastUpdate: nowMeta(opts.Clock),
	}
	if state.status.needToSetSourceStatus(newSourceStatus) {
		klog.V(3).Info("Updating source status (after parse)")
		if statusErr := r.syncStatusClient.SetSourceStatus(ctx, newSourceStatus); statusErr != nil {
			return status.Append(parseErrs, statusErr)
		}
		state.status.SourceStatus = newSourceStatus
	}
	return parseErrs
}

// update syncs the objects with known scope to the cluster.
func (r *reconciler) update(ctx context.Context, trigger string) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()

	asyncCtx, asyncCancel := context.WithCancel(ctx)
	asyncDoneCh := r.startAsyncStatusUpdates(asyncCtx)

	klog.V(3).Info("Updater starting...")
	start := opts.Clock.Now()
	updateErrs := opts.Update(ctx, &state.cache)
	metrics.RecordParserDuration(ctx, trigger, "update", metrics.StatusTagKey(updateErrs), start)
	klog.V(3).Info("Updater stopped")

	// Stop periodic updates
	asyncCancel()
	// Wait for periodic updates to stop
	<-asyncDoneCh

	// SyncErrors include errors from both the Updater and Remediator
	klog.V(3).Info("Updating sync status (after sync)")
	syncErrs := r.ReconcilerState().SyncErrors()

	// Copy the spec and commit from the source status
	syncStatus := &SyncStatus{
		Spec:       state.status.SourceStatus.Spec,
		Syncing:    false,
		Commit:     state.cache.source.commit,
		Errs:       syncErrs,
		LastUpdate: nowMeta(opts.Clock),
	}
	if statusErr := r.setSyncStatus(ctx, syncStatus); statusErr != nil {
		return status.Append(syncErrs, statusErr)
	}
	return syncErrs
}

// setSyncStatus updates `.status.sync` and the Syncing condition, if needed,
// as well as `state.SyncStatus` if the update is successful.
func (r *reconciler) setSyncStatus(ctx context.Context, newSyncStatus *SyncStatus) error {
	state := r.ReconcilerState()
	// Update the RSync status, if necessary
	if state.status.needToSetSyncStatus(newSyncStatus) {
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

// startAsyncStatusUpdates starts a goroutine that updates the sync status
// periodically until the context is cancelled. The caller should wait until the
// done channel is closed to confirm the goroutine has exited.
func (r *reconciler) startAsyncStatusUpdates(ctx context.Context) <-chan struct{} {
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
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
				syncStatus := &SyncStatus{
					Spec:       state.status.SourceStatus.Spec,
					Syncing:    true,
					Commit:     state.cache.source.commit,
					Errs:       state.SyncErrors(),
					LastUpdate: nowMeta(opts.Clock),
				}
				if err := r.setSyncStatus(ctx, syncStatus); err != nil {
					klog.Warningf("failed to update sync status: %v", err)
				}

				updateTimer.Reset(updatePeriod) // Schedule status update attempt
			}
		}
	}()
	return doneCh
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
			if err := prependRootSyncRemediatorStatus(ctx, k8sClient, name, conflictErrors, defaultDenominator); err != nil {
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
	if !opts.Remediating() {
		return nil
	}
	klog.V(3).Info("Updating sync status (periodic while not syncing)")
	state := r.ReconcilerState()
	// Don't update the sync spec or commit, just the errors and status.
	syncStatus := &SyncStatus{
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
