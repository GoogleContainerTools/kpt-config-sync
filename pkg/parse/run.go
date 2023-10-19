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
	"fmt"
	"os"
	"path"
	"time"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
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

// Trigger is a type alias of the Syncer triggers.
type Trigger string

const (
	// Resync is triggered by the resync timer (every hour)
	Resync Trigger = "resync"
	// Reimport is triggered by the reimport timer (default to 15s)
	Reimport Trigger = "reimport"
	// Retry is triggered by the retry timer with exponential backoff
	Retry Trigger = "retry"
	// ManagementConflict is a special type of retry, which represents conflicts between controllers
	ManagementConflict Trigger = "managementConflict"
	// WatchUpdate is another special type of retry, representing the Remediator watchers need to be updated.
	//This is only used as a tag of the parser_duration_seconds metric.
	WatchUpdate Trigger = "watchUpdate"
	// StatusUpdate is triggered to update the sync status to report management conflicts from the Remediator (every 5s)
	StatusUpdate Trigger = "statusUpdate"
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

// Run is the entry point of the parse-apply-watch loop.
func Run(ctx context.Context, p Parser, trigger Trigger, state *ReconcilerState) {
	var syncDir cmpath.Absolute
	gs := sourceStatus{}
	// pull the source commit and directory with retries within 5 minutes.
	gs.commit, syncDir, gs.errs = hydrate.SourceCommitAndDirWithRetry(util.SourceRetryBackoff, p.options().SourceType, p.options().SourceDir, p.options().SyncDir, p.options().reconcilerName)

	// If failed to fetch the source commit and directory, set `.status.source` to fail early.
	// Otherwise, set `.status.rendering` before `.status.source` because the parser needs to
	// read and parse the configs after rendering is done and there might have errors.
	if gs.errs != nil {
		gs.lastUpdate = metav1.Now()
		var setSourceStatusErr error
		if state.needToSetSourceStatus(gs) {
			klog.V(3).Infof("Updating source status (before read): %#v", gs)
			setSourceStatusErr = p.setSourceStatus(ctx, gs)
			if setSourceStatusErr == nil {
				state.sourceStatus = gs
				state.syncingConditionLastUpdate = gs.lastUpdate
			}
		}
		state.invalidate(status.Append(gs.errs, setSourceStatusErr))
		return
	}

	rs := renderingStatus{
		commit:            gs.commit,
		requiresRendering: state.renderingStatus.requiresRendering,
	}

	// set the rendering status by checking the done file.
	if p.options().renderingEnabled {
		doneFilePath := p.options().RepoRoot.Join(cmpath.RelativeSlash(hydrate.DoneFile)).OSPath()
		_, err := os.Stat(doneFilePath)
		if os.IsNotExist(err) || (err == nil && hydrate.DoneCommit(doneFilePath) != gs.commit) {
			rs.message = RenderingInProgress
			rs.lastUpdate = metav1.Now()
			klog.V(3).Infof("Updating rendering status (before read): %#v", rs)
			setRenderingStatusErr := p.setRenderingStatus(ctx, state.renderingStatus, rs)
			if setRenderingStatusErr == nil {
				state.reset()
				state.renderingStatus = rs
				state.syncingConditionLastUpdate = rs.lastUpdate
			} else {
				var m status.MultiError
				state.invalidate(status.Append(m, setRenderingStatusErr))
			}
			return
		}
		if err != nil {
			rs.message = RenderingFailed
			rs.lastUpdate = metav1.Now()
			rs.errs = status.InternalHydrationError(err, "unable to read the done file: %s", doneFilePath)
			klog.V(3).Infof("Updating rendering status (before read): %#v", rs)
			setRenderingStatusErr := p.setRenderingStatus(ctx, state.renderingStatus, rs)
			if setRenderingStatusErr == nil {
				state.renderingStatus = rs
				state.syncingConditionLastUpdate = rs.lastUpdate
			}
			state.invalidate(status.Append(rs.errs, setRenderingStatusErr))
			return
		}
	}

	// rendering is done, starts to read the source or hydrated configs.
	oldSyncDir := state.cache.source.syncDir
	// `read` is called no matter what the trigger is.
	ps := sourceState{
		commit:  gs.commit,
		syncDir: syncDir,
	}
	if errs := read(ctx, p, trigger, state, ps); errs != nil {
		state.invalidate(errs)
		return
	}

	newSyncDir := state.cache.source.syncDir

	if newSyncDir != oldSyncDir {
		// Reset the backoff and retryTimer since it is a new commit
		state.ResetRetryBackoffAndTimer()
	}

	// The parse-apply-watch sequence will be skipped if the trigger type is `Reimport` and
	// there is no new source changes. The reasons are:
	//   * If a former parse-apply-watch sequence for syncDir succeeded, there is no need to run the sequence again;
	//   * If all the former parse-apply-watch sequences for syncDir failed, the next retry will call the sequence.
	if trigger == Reimport && oldSyncDir == newSyncDir {
		return
	}

	errs := parseAndUpdate(ctx, p, trigger, state)
	if errs != nil {
		state.invalidate(errs)
		return
	}

	// Only checkpoint the state after *everything* succeeded, including status update.
	state.checkpoint()
}

// read reads config files from source if no rendering is needed, or from hydrated output if rendering is done.
// It also updates the .status.rendering and .status.source fields.
func read(ctx context.Context, p Parser, trigger Trigger, state *ReconcilerState, sourceState sourceState) status.MultiError {
	hydrationStatus, sourceStatus := readFromSource(ctx, p, trigger, state, sourceState)
	if p.options().renderingEnabled != hydrationStatus.requiresRendering {
		// the reconciler is misconfigured. set the annotation so that the reconciler-manager
		// will recreate this reconciler with the correct configuration.
		if err := p.setRequiresRendering(ctx, hydrationStatus.requiresRendering); err != nil {
			hydrationStatus.errs = status.Append(hydrationStatus.errs,
				status.InternalHydrationError(err, "error setting %s annotation", metadata.RequiresRenderingAnnotationKey))
		}
	}
	hydrationStatus.lastUpdate = metav1.Now()
	// update the rendering status before source status because the parser needs to
	// read and parse the configs after rendering is done and there might have errors.
	klog.V(3).Infof("Updating rendering status (after read): %#v", hydrationStatus)
	setRenderingStatusErr := p.setRenderingStatus(ctx, state.renderingStatus, hydrationStatus)
	if setRenderingStatusErr == nil {
		state.renderingStatus = hydrationStatus
		state.syncingConditionLastUpdate = hydrationStatus.lastUpdate
	}
	renderingErrs := status.Append(hydrationStatus.errs, setRenderingStatusErr)
	if renderingErrs != nil {
		return renderingErrs
	}

	if sourceStatus.errs == nil {
		return nil
	}

	// Only call `setSourceStatus` if `readFromSource` fails.
	// If `readFromSource` succeeds, `parse` may still fail.
	sourceStatus.lastUpdate = metav1.Now()
	var setSourceStatusErr error
	if state.needToSetSourceStatus(sourceStatus) {
		klog.V(3).Infof("Updating source status (after read): %#v", sourceStatus)
		setSourceStatusErr := p.setSourceStatus(ctx, sourceStatus)
		if setSourceStatusErr == nil {
			state.sourceStatus = sourceStatus
			state.syncingConditionLastUpdate = sourceStatus.lastUpdate
		}
	}

	return status.Append(sourceStatus.errs, setSourceStatusErr)
}

// parseHydrationState reads from the file path which the hydration-controller
// container writes to. It checks if the hydrated files are ready and returns
// a renderingStatus.
func parseHydrationState(p Parser, srcState sourceState, hydrationStatus renderingStatus) (sourceState, renderingStatus) {
	options := p.options()
	if !options.renderingEnabled {
		hydrationStatus.message = RenderingSkipped
		return srcState, hydrationStatus
	}
	// Check if the hydratedRoot directory exists.
	// If exists, read the hydrated directory. Otherwise, fail.
	absHydratedRoot, err := cmpath.AbsoluteOS(options.HydratedRoot)
	if err != nil {
		hydrationStatus.message = RenderingFailed
		hydrationStatus.errs = status.InternalHydrationError(err, "hydrated-dir must be an absolute path")
		return srcState, hydrationStatus
	}

	var hydrationErr hydrate.HydrationError
	if _, err := os.Stat(absHydratedRoot.OSPath()); err == nil {
		// pull the hydrated commit and directory with retries within 1 minute.
		srcState, hydrationErr = options.readHydratedDirWithRetry(util.HydratedRetryBackoff, absHydratedRoot, options.reconcilerName, srcState)
		if hydrationErr != nil {
			hydrationStatus.message = RenderingFailed
			hydrationStatus.errs = status.HydrationError(hydrationErr.Code(), hydrationErr)
			return srcState, hydrationStatus
		}
		hydrationStatus.message = RenderingSucceeded
	} else if !os.IsNotExist(err) {
		hydrationStatus.message = RenderingFailed
		hydrationStatus.errs = status.InternalHydrationError(err, "unable to evaluate the hydrated path %s", absHydratedRoot.OSPath())
		return srcState, hydrationStatus
	} else {
		// Source of truth does not require hydration, but hydration-controller is running
		hydrationStatus.message = RenderingNotRequired
		hydrationStatus.requiresRendering = false
		err := hydrate.NewTransientError(fmt.Errorf("sync source contains only wet configs and hydration-controller is running"))
		hydrationStatus.errs = status.HydrationError(err.Code(), err)
		return srcState, hydrationStatus
	}
	return srcState, hydrationStatus
}

// readFromSource reads the source or hydrated configs, checks whether the sourceState in
// the cache is up-to-date. If the cache is not up-to-date, reads all the source or hydrated files.
// readFromSource returns the rendering status and source status.
func readFromSource(ctx context.Context, p Parser, trigger Trigger, recState *ReconcilerState, srcState sourceState) (renderingStatus, sourceStatus) {
	options := p.options()
	start := time.Now()

	hydrationStatus := renderingStatus{
		commit:            srcState.commit,
		requiresRendering: options.renderingEnabled,
	}
	srcStatus := sourceStatus{
		commit: srcState.commit,
	}

	srcState, hydrationStatus = parseHydrationState(p, srcState, hydrationStatus)
	if hydrationStatus.errs != nil {
		return hydrationStatus, srcStatus
	}

	if srcState.syncDir == recState.cache.source.syncDir {
		return hydrationStatus, srcStatus
	}

	// Read all the files under srcState.syncDir
	srcStatus.errs = options.readConfigFiles(&srcState)

	if !options.renderingEnabled {
		// Check if any kustomization files exist
		for _, fi := range srcState.files {
			if hydrate.HasKustomization(path.Base(fi.OSPath())) {
				// Source of truth requires hydration, but the hydration-controller is not running
				hydrationStatus.message = RenderingRequired
				hydrationStatus.requiresRendering = true
				err := hydrate.NewTransientError(fmt.Errorf("sync source contains dry configs and hydration-controller is not running"))
				hydrationStatus.errs = status.HydrationError(err.Code(), err)
				return hydrationStatus, srcStatus
			}
		}
	}

	klog.Infof("New source changes (%s) detected, reset the cache", srcState.syncDir.OSPath())
	// Reset the cache to make sure all the steps of a parse-apply-watch loop will run.
	recState.resetCache()
	if srcStatus.errs == nil {
		// Set `state.cache.source` after `readConfigFiles` succeeded
		recState.cache.source = srcState
	}
	metrics.RecordParserDuration(ctx, string(trigger), "read", metrics.StatusTagKey(srcStatus.errs), start)
	return hydrationStatus, srcStatus
}

func parseSource(ctx context.Context, p Parser, trigger Trigger, state *ReconcilerState) status.MultiError {
	if state.cache.parserResultUpToDate() {
		return nil
	}

	start := time.Now()
	objs, sourceErrs := p.parseSource(ctx, state.cache.source)
	metrics.RecordParserDuration(ctx, string(trigger), "parse", metrics.StatusTagKey(sourceErrs), start)
	state.cache.setParserResult(objs, sourceErrs)

	if !status.HasBlockingErrors(sourceErrs) {
		err := webhookconfiguration.Update(ctx, p.options().k8sClient(), p.options().discoveryClient(), objs)
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

func parseAndUpdate(ctx context.Context, p Parser, trigger Trigger, state *ReconcilerState) status.MultiError {
	klog.V(3).Info("Parser starting...")
	sourceErrs := parseSource(ctx, p, trigger, state)
	klog.V(3).Info("Parser stopped")
	newSourceStatus := sourceStatus{
		commit:     state.cache.source.commit,
		errs:       sourceErrs,
		lastUpdate: metav1.Now(),
	}
	if state.needToSetSourceStatus(newSourceStatus) {
		klog.V(3).Infof("Updating source status (after parse): %#v", newSourceStatus)
		if err := p.setSourceStatus(ctx, newSourceStatus); err != nil {
			// If `p.setSourceStatus` fails, we terminate the reconciliation.
			// If we call `update` in this case and `update` succeeds, `Status.Source.Commit` would end up be older
			// than `Status.Sync.Commit`.
			return status.Append(sourceErrs, err)
		}
		state.sourceStatus = newSourceStatus
		state.syncingConditionLastUpdate = newSourceStatus.lastUpdate
	}

	if status.HasBlockingErrors(sourceErrs) {
		return sourceErrs
	}

	// Create a new context with its cancellation function.
	ctxForUpdateSyncStatus, cancel := context.WithCancel(context.Background())

	go updateSyncStatusPeriodically(ctxForUpdateSyncStatus, p, state)

	klog.V(3).Info("Updater starting...")
	start := time.Now()
	syncErrs := p.options().Update(ctx, &state.cache)
	metrics.RecordParserDuration(ctx, string(trigger), "update", metrics.StatusTagKey(syncErrs), start)
	klog.V(3).Info("Updater stopped")

	// This is to terminate `updateSyncStatusPeriodically`.
	cancel()

	klog.V(3).Info("Updating sync status (after sync)")
	if err := SetSyncStatus(ctx, p, state, false, syncErrs); err != nil {
		syncErrs = status.Append(syncErrs, err)
	}

	return status.Append(sourceErrs, syncErrs)
}

// SetSyncStatus updates `.status.sync` and the Syncing condition, if needed,
// as well as `state.syncStatus` and `state.syncingConditionLastUpdate` if
// the update is successful.
func SetSyncStatus(ctx context.Context, p Parser, state *ReconcilerState, syncing bool, syncErrs status.MultiError) error {
	// Update the RSync status, if necessary
	newSyncStatus := syncStatus{
		syncing:    syncing,
		commit:     state.cache.source.commit,
		errs:       syncErrs,
		lastUpdate: metav1.Now(),
	}
	if state.needToSetSyncStatus(newSyncStatus) {
		if err := p.SetSyncStatus(ctx, newSyncStatus); err != nil {
			return err
		}
		state.syncStatus = newSyncStatus
		state.syncingConditionLastUpdate = newSyncStatus.lastUpdate
	}

	// Extract conflict errors from sync errors.
	var conflictErrs []status.ManagementConflictError
	if syncErrs != nil {
		for _, err := range syncErrs.Errors() {
			if conflictErr, ok := err.(status.ManagementConflictError); ok {
				conflictErrs = append(conflictErrs, conflictErr)
			}
		}
	}
	// Report conflict errors to the remote manager, if it's a RootSync.
	if err := reportRootSyncConflicts(ctx, p.K8sClient(), conflictErrs); err != nil {
		return errors.Wrapf(err, "failed to report remote conflicts")
	}
	return nil
}

// updateSyncStatusPeriodically update the sync status periodically until the
// cancellation function of the context is called.
func updateSyncStatusPeriodically(ctx context.Context, p Parser, state *ReconcilerState) {
	klog.V(3).Info("Periodic sync status updates starting...")
	updatePeriod := p.options().statusUpdatePeriod
	updateTimer := time.NewTimer(updatePeriod)
	defer updateTimer.Stop()
	for {
		select {
		case <-ctx.Done():
			// ctx.Done() is closed when the cancellation function of the context is called.
			klog.V(3).Info("Periodic sync status updates stopped")
			return

		case <-updateTimer.C:
			klog.V(3).Info("Updating sync status (periodic while syncing)")
			if err := SetSyncStatus(ctx, p, state, true, p.SyncErrors()); err != nil {
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
		conflictingManager := conflictError.ConflictingManager()
		err := conflictError.ConflictingManagerError()
		conflictingManagerErrors[conflictingManager] = append(conflictingManagerErrors[conflictingManager], err)
	}

	for conflictingManager, conflictErrors := range conflictingManagerErrors {
		scope, name := declared.ManagerScopeAndName(conflictingManager)
		if scope == declared.RootReconciler {
			// RootSync applier uses PolicyAdoptAll.
			// So it may fight, if the webhook is disabled.
			// Report the conflict to the other RootSync to make it easier to detect.
			klog.Infof("Detected conflict with RootSync manager %q", conflictingManager)
			if err := prependRootSyncRemediatorStatus(ctx, k8sClient, name, conflictErrors, defaultDenominator); err != nil {
				return errors.Wrapf(err, "failed to update RootSync %q to prepend remediator conflicts", name)
			}
		} else {
			// RepoSync applier uses PolicyAdoptIfNoInventory.
			// So it won't fight, even if the webhook is disabled.
			klog.Infof("Detected conflict with RepoSync manager %q", conflictingManager)
		}
	}
	return nil
}
