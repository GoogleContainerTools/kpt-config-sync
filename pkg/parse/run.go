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

// RunResult encapsulates the result of a RunFunc.
// This simply allows explicitly naming return values in a way that makes the
// implementation easier to read.
type RunResult struct {
	SourceChanged bool
	Success       bool
}

// RunFunc is the function signature of the function that starts the parse-apply-watch loop
type RunFunc func(ctx context.Context, r Reconciler, trigger string) RunResult

// DefaultRunFunc is the default implementation for RunOpts.RunFunc.
func DefaultRunFunc(ctx context.Context, r Reconciler, trigger string) RunResult {
	klog.Infof("Starting sync attempt (trigger: %s)", trigger)
	opts := r.Options()
	result := RunResult{}
	state := r.ReconcilerState()
	// Initialize status
	// TODO: Populate status from RSync status
	if state.status == nil {
		reconcilerStatus, err := r.SyncStatusClient().GetReconcilerStatus(ctx)
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

	var syncPath cmpath.Absolute
	newSourceStatus := &SourceStatus{}
	// pull the source commit and directory with retries within 5 minutes.
	newSourceStatus.Commit, syncPath, newSourceStatus.Errs = hydrate.SourceCommitAndSyncPathWithRetry(util.SourceRetryBackoff, opts.SourceType, opts.SourceDir, opts.SyncDir, opts.ReconcilerName)

	// Add pre-sync annotations to the object.
	// If updating the object fails, it's likely due to a signature verification error
	// from the webhook. In this case, add the error as a source error.
	if newSourceStatus.Errs == nil {
		if err := r.SyncStatusClient().SetImageToSyncAnnotation(ctx, newSourceStatus.Commit); err != nil {
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
		// Only update the source status if it changed
		if state.status.needToSetSourceStatus(newSourceStatus) {
			klog.V(3).Info("Updating source status (after fetch)")
			if statusErr := r.SyncStatusClient().SetSourceStatus(ctx, newSourceStatus); statusErr != nil {
				state.RecordFailure(opts.Clock, status.Append(newSourceStatus.Errs, statusErr))
				return result
			}
			state.status.SourceStatus = newSourceStatus
		}
		// If there were fetch errors, stop, log them, and retry later
		if newSourceStatus.Errs != nil {
			state.RecordFailure(opts.Clock, newSourceStatus.Errs)
			return result
		}
	}

	// set the rendering status by checking the done file.
	if opts.RenderingEnabled {
		if errs := r.Render(ctx, newSourceStatus); errs != nil {
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
	// `read` is called no matter what the trigger is.
	if errs := r.Read(ctx, trigger, newSourceStatus, syncPath); errs != nil {
		state.RecordFailure(opts.Clock, errs)
		return result
	}

	newSyncPath := state.cache.source.syncPath

	if newSyncPath != oldSyncPath {
		// If the commit changed and parsing succeeded, trigger retries to start again, if stopped.
		result.SourceChanged = true
	}

	// The parse-apply-watch sequence will be skipped if the trigger type is `triggerReimport` and
	// there is no new source changes. The reasons are:
	//   * If a former parse-apply-watch sequence for syncPath succeeded, there is no need to run the sequence again;
	//   * If all the former parse-apply-watch sequences for syncPath failed, the next retry will call the sequence.
	if trigger == triggerSync && oldSyncPath == newSyncPath {
		return result
	}

	errs := r.ParseAndUpdate(ctx, trigger)
	if errs != nil {
		state.RecordFailure(opts.Clock, errs)
		return result
	}

	// Only checkpoint the state after *everything* succeeded, including status update.
	state.RecordSyncSuccess(opts.Clock)
	result.Success = true
	return result
}

// Render waits for the hydration-controller sidecar to render the source
// manifests on the shared source volume.
// Updates the RSync status (rendering status and syncing condition).
func (r *reconciler) Render(ctx context.Context, sourceStatus *SourceStatus) status.MultiError {
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
		klog.V(3).Info("Updating rendering status (before parse)")
		if statusErr := r.SyncStatusClient().SetRenderingStatus(ctx, state.status.RenderingStatus, newRenderStatus); statusErr != nil {
			return status.Append(newRenderStatus.Errs, statusErr)
		}
		state.status.RenderingStatus = newRenderStatus
		return newRenderStatus.Errs
	}
	if renderedCommit == "" || renderedCommit != sourceStatus.Commit {
		newRenderStatus.Message = RenderingInProgress
		newRenderStatus.LastUpdate = nowMeta(opts.Clock)
		klog.V(3).Info("Updating rendering status (before parse)")
		if statusErr := r.SyncStatusClient().SetRenderingStatus(ctx, state.status.RenderingStatus, newRenderStatus); statusErr != nil {
			return statusErr
		}
		state.status.RenderingStatus = newRenderStatus
		state.RecordRenderInProgress()
		// Transient error will be logged as info, instead of error,
		// but still trigger retry with backoff.
		return status.TransientError(errors.New(RenderingInProgress))
	}
	return nil
}

// Read source manifests from the shared source volume.
// Waits for rendering, if enabled.
// Updates the RSync status (source, rendering, and syncing condition).
func (r *reconciler) Read(ctx context.Context, trigger string, sourceStatus *SourceStatus, syncPath cmpath.Absolute) status.MultiError {
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
		if err := r.SyncStatusClient().SetRequiresRenderingAnnotation(ctx, newRenderStatus.RequiresRendering); err != nil {
			newRenderStatus.Errs = status.Append(newRenderStatus.Errs, err)
		}
	}
	newRenderStatus.LastUpdate = nowMeta(opts.Clock)
	// update the rendering status before source status because the parser needs to
	// read and parse the configs after rendering is done and there might have errors.
	klog.V(3).Info("Updating rendering status (after parse)")
	if statusErr := r.SyncStatusClient().SetRenderingStatus(ctx, state.status.RenderingStatus, newRenderStatus); statusErr != nil {
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
			klog.V(3).Info("Updating source status (after parse)")
			if statusErr := r.SyncStatusClient().SetSourceStatus(ctx, newSourceStatus); statusErr != nil {
				return status.Append(newSourceStatus.Errs, statusErr)
			}
			state.status.SourceStatus = newSourceStatus
		}
		return newSourceStatus.Errs
	}

	// Read succeeded
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

func (r *reconciler) parseSource(ctx context.Context, trigger string) status.MultiError {
	state := r.ReconcilerState()
	if state.cache.parserResultUpToDate() {
		return nil
	}

	opts := r.Options()
	start := opts.Clock.Now()
	var sourceErrs status.MultiError

	objs, errs := r.Parser().ParseSource(ctx, state.cache.source)
	if !opts.WebhookEnabled {
		klog.V(3).Infof("Removing %s annotation as Admission Webhook is disabled", metadata.DeclaredFieldsKey)
		for _, obj := range objs {
			core.RemoveAnnotations(obj, metadata.DeclaredFieldsKey)
		}
	}
	sourceErrs = status.Append(sourceErrs, errs)
	metrics.RecordParserDuration(ctx, trigger, "parse", metrics.StatusTagKey(sourceErrs), start)
	state.cache.setParserResult(objs, sourceErrs)

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

// ParseAndUpdate parses objects from the source manifests, validates them, and
// then syncs them to the cluster with the Updater.
func (r *reconciler) ParseAndUpdate(ctx context.Context, trigger string) status.MultiError {
	opts := r.Options()
	state := r.ReconcilerState()
	klog.V(3).Info("Parser starting...")
	sourceErrs := r.parseSource(ctx, trigger)
	klog.V(3).Info("Parser stopped")
	newSourceStatus := &SourceStatus{
		Spec:       state.cache.source.spec,
		Commit:     state.cache.source.commit,
		Errs:       sourceErrs,
		LastUpdate: nowMeta(opts.Clock),
	}
	if state.status.needToSetSourceStatus(newSourceStatus) {
		klog.V(3).Info("Updating source status (after parse)")
		if statusErr := r.SyncStatusClient().SetSourceStatus(ctx, newSourceStatus); statusErr != nil {
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
	syncStatus := &SyncStatus{
		Spec:       state.status.SourceStatus.Spec,
		Syncing:    false,
		Commit:     state.cache.source.commit,
		Errs:       syncErrs,
		LastUpdate: nowMeta(opts.Clock),
	}
	if statusErr := r.SetSyncStatus(ctx, syncStatus); statusErr != nil {
		return status.Append(reconcileErrs, statusErr)
	}
	return reconcileErrs
}

// SetSyncStatus updates `.status.sync` and the Syncing condition, if needed,
// as well as `state.SyncStatus` if the update is successful.
func (r *reconciler) SetSyncStatus(ctx context.Context, newSyncStatus *SyncStatus) error {
	state := r.ReconcilerState()
	// Update the RSync status, if necessary
	if state.status.needToSetSyncStatus(newSyncStatus) {
		if statusErr := r.SyncStatusClient().SetSyncStatus(ctx, newSyncStatus); statusErr != nil {
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
			syncStatus := &SyncStatus{
				Spec:       state.status.SourceStatus.Spec,
				Syncing:    true,
				Commit:     state.cache.source.commit,
				Errs:       r.ReconcilerState().SyncErrors(),
				LastUpdate: nowMeta(opts.Clock),
			}
			if err := r.SetSyncStatus(ctx, syncStatus); err != nil {
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

func nowMeta(c clock.Clock) metav1.Time {
	return metav1.Time{Time: c.Now()}
}
