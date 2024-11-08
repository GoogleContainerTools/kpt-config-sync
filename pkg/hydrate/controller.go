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

package hydrate

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util"
)

const (
	// tmpLink is the temporary soft link name.
	tmpLink = "tmp-link"
	// DoneFile is the file name that indicates the hydration is done.
	DoneFile = "done"
	// ErrorFile is the file name of the hydration errors.
	ErrorFile = "error.json"
)

// Hydrator runs the hydration process.
type Hydrator struct {
	// DonePath is the absolute path to the done file under the /repo directory.
	DonePath cmpath.Absolute
	// SourceType is the type of the source repository, must be git or oci.
	SourceType configsync.SourceType
	// SourceRoot is the absolute path to the source root directory.
	SourceRoot cmpath.Absolute
	// HydratedRoot is the absolute path to the hydrated root directory.
	HydratedRoot cmpath.Absolute
	// SourceLink is the name of (a symlink to) the source directory under SourceRoot, which contains the clone of the git repo.
	SourceLink string
	// HydratedLink is the name of (a symlink to) the source directory under HydratedRoot, which contains the hydrated configs.
	HydratedLink string
	// SyncDir is the relative path to the configs within the Git repository.
	SyncDir cmpath.Relative
	// PollingPeriod is the period of time between checking the filesystem for source updates to render.
	PollingPeriod time.Duration
	// RehydratePeriod is the period of time between rehydrating on errors.
	RehydratePeriod time.Duration
	// ReconcilerName is the name of the reconciler.
	ReconcilerName string
}

// Run runs the hydration process periodically.
func (h *Hydrator) Run(ctx context.Context) {
	// Use timers, not tickers.
	// Tickers can cause memory leaks and continuous execution, when execution
	// takes longer than the tick duration.
	runTimer := time.NewTimer(h.PollingPeriod)
	defer runTimer.Stop()
	rehydrateTimer := time.NewTimer(h.RehydratePeriod)
	defer rehydrateTimer.Stop()
	// sourcePath is the path to which the source was fetched
	sourcePath := h.sourcePath()
	// srcCommit is the git commit, oci checksum, or helm version that was fetched
	var srcCommit string
	// syncPath is the path from which the applier should sync
	var syncPath cmpath.Absolute
	var hydrateErr HydrationError
	var fetchErr error
	for {
		select {
		case <-ctx.Done():
			return
		case <-rehydrateTimer.C:
			hydrateErr = h.rehydrateOnError(hydrateErr, srcCommit, syncPath)
			rehydrateTimer.Reset(h.RehydratePeriod) // Schedule rehydrate attempt
		case <-runTimer.C:
			// pull the source commit and directory with retries within 5 minutes.
			srcCommit, syncPath, fetchErr = SourceCommitAndSyncPathWithRetry(util.SourceRetryBackoff, h.SourceType, sourcePath, h.SyncDir, h.ReconcilerName)
			if fetchErr != nil {
				hydrateErr = NewInternalError(fmt.Errorf("failed to get the commit hash and sync directory from the source directory %s: %w", sourcePath.OSPath(), fetchErr))
				if err := h.complete(srcCommit, hydrateErr); err != nil {
					klog.Errorf("failed to complete the rendering execution for commit %q: %v",
						srcCommit, err)
				}
			} else if doneCommit(h.DonePath.OSPath()) != srcCommit {
				// If the commit has been processed before, regardless of success or failure,
				// skip the hydration to avoid repeated execution.
				// The rehydrate ticker will retry on the failed commit.
				hydrateErr = h.hydrate(srcCommit, syncPath)
				if err := h.complete(srcCommit, hydrateErr); err != nil {
					klog.Errorf("failed to complete the rendering execution for commit %q: %v", srcCommit, err)
				}
			}
			runTimer.Reset(h.PollingPeriod) // Schedule re-run attempt
		}
	}
}

// runHydrate runs `kustomize build` on the source configs.
func (h *Hydrator) runHydrate(sourceCommit string, syncPath cmpath.Absolute) HydrationError {
	newHydratedDir := h.HydratedRoot.Join(cmpath.RelativeOS(sourceCommit))
	dest := newHydratedDir.Join(h.SyncDir).OSPath()

	osSyncPath := syncPath.OSPath()
	if err := kustomizeBuild(osSyncPath, dest, true); err != nil {
		return err
	}

	newCommit, err := ComputeCommit(h.sourcePath())
	if err != nil {
		return NewTransientError(err)
	} else if sourceCommit != newCommit {
		return NewTransientError(fmt.Errorf("source commit changed while running Kustomize build, was %s, now %s. It will be retried in the next sync", sourceCommit, newCommit))
	}

	if err := updateSymlink(h.HydratedRoot.OSPath(), h.HydratedLink, newHydratedDir.OSPath()); err != nil {
		return NewInternalError(fmt.Errorf("unable to update the symbolic link to %s: %w", newHydratedDir.OSPath(), err))
	}
	klog.Infof("Successfully rendered %s for commit %s", osSyncPath, sourceCommit)
	return nil
}

// ComputeCommit returns the computed commit from given sourceDir, or error
// if the sourceDir fails symbolic link evaluation
func ComputeCommit(sourceDir cmpath.Absolute) (string, error) {
	dir, err := sourceDir.EvalSymlinks()
	if err != nil {
		return "", fmt.Errorf("unable to evaluate the symbolic link of sourceDir %s: %w", dir, err)
	}
	newCommit := filepath.Base(dir.OSPath())
	return newCommit, nil
}

// sourcePath returns the absolute path of a source directory by joining the
// absolute source root path and a relative source directory path.
func (h *Hydrator) sourcePath() cmpath.Absolute {
	return h.SourceRoot.Join(cmpath.RelativeSlash(h.SourceLink))
}

// hydrate renders the source git repo to hydrated configs.
func (h *Hydrator) hydrate(sourceCommit string, syncPath cmpath.Absolute) HydrationError {
	osSyncPath := syncPath.OSPath()
	hydrate, err := needsKustomize(osSyncPath)
	if err != nil {
		return NewInternalError(fmt.Errorf("unable to check if rendering is needed for the source directory: %s: %w", osSyncPath, err))
	}
	if !hydrate {
		found, err := hasKustomizeSubdir(osSyncPath)
		if err != nil {
			return NewInternalError(err)
		}
		if found {
			return NewActionableError(fmt.Errorf("Kustomization config file is missing from the sync directory %s. "+
				"To fix, either add kustomization.yaml in the sync directory to trigger the rendering process, "+
				"or remove kustomizaiton.yaml from all sub directories to skip rendering.", osSyncPath))
		}
		klog.V(5).Infof("no rendering is needed because of no Kustomization config file in the source configs with commit %s", sourceCommit)
		if err := os.RemoveAll(h.HydratedRoot.OSPath()); err != nil {
			return NewInternalError(err)
		}
		return nil
	}

	// Remove the done file because a new hydration is in progress.
	if err := os.RemoveAll(h.DonePath.OSPath()); err != nil {
		return NewInternalError(fmt.Errorf("unable to remove the done file: %s: %w", h.DonePath.OSPath(), err))
	}
	return h.runHydrate(sourceCommit, syncPath)
}

// rehydrateOnError is triggered by the rehydrateTimer (every 30 mins)
// It re-runs the rendering process when there is a previous error.
func (h *Hydrator) rehydrateOnError(prevErr HydrationError, prevSrcCommit string, prevSyncPath cmpath.Absolute) HydrationError {
	if prevErr == nil {
		// Return directly if the previous hydration succeeded.
		return nil
	}
	if len(prevSrcCommit) == 0 || len(prevSyncPath) == 0 {
		// If source commit and directory isn't available, skip rehydrating because
		// rehydrateOnError is supposed to run after h.hydrate processes the commit.
		return prevErr
	}
	klog.Infof("retry rendering commit %s", prevSrcCommit)
	hydrationErr := h.runHydrate(prevSrcCommit, prevSyncPath)
	if err := h.complete(prevSrcCommit, hydrationErr); err != nil {
		klog.Errorf("failed to complete the re-rendering execution for commit %q: %v", prevSrcCommit, err)
	}
	return hydrationErr
}

// updateSymlink updates the symbolic link to the hydrated directory.
func updateSymlink(hydratedRoot, link, newDir string) error {
	linkPath := filepath.Join(hydratedRoot, link)
	tmpLinkPath := filepath.Join(hydratedRoot, tmpLink)

	oldDir, err := filepath.EvalSymlinks(linkPath)
	if oldDir == newDir {
		return nil
	}
	deleteOldDir := true
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("unable to access the current hydrated directory: %s: %w", linkPath, err)
		}
		deleteOldDir = false
	}

	if err := os.Symlink(newDir, tmpLinkPath); err != nil {
		return fmt.Errorf("unable to create symlink: %w", err)
	}

	if err := os.Rename(tmpLinkPath, linkPath); err != nil {
		return fmt.Errorf("unable to replace symlink: %w", err)
	}

	if deleteOldDir {
		if err := os.RemoveAll(oldDir); err != nil {
			klog.Warningf("unable to remove the previously hydrated directory %s: %v", oldDir, err)
		}
	}
	return nil
}

// complete marks the hydration process is done with a done file under the /repo directory
// and reset the error file (create, update or delete).
func (h *Hydrator) complete(commit string, hydrationErr HydrationError) error {
	errorPath := h.HydratedRoot.Join(cmpath.RelativeSlash(ErrorFile)).OSPath()
	var err error
	if hydrationErr == nil {
		err = deleteErrorFile(errorPath)
	} else {
		err = exportError(commit, h.HydratedRoot.OSPath(), errorPath, hydrationErr)
	}
	if err != nil {
		return err
	}
	done, err := os.Create(h.DonePath.OSPath())
	if err != nil {
		return fmt.Errorf("unable to create done file: %s: %w", h.DonePath.OSPath(), err)
	}
	if _, err = done.WriteString(commit); err != nil {
		return fmt.Errorf("unable to write to commit hash to the done file: %s: %w", h.DonePath, err)
	}
	if err := done.Close(); err != nil {
		klog.Warningf("unable to close the done file %s: %v", h.DonePath.OSPath(), err)
	}
	return nil
}

// doneCommit extracts the commit hash from the done file if exists.
// It returns the commit hash if exists, otherwise, returns an empty string.
// If it fails to extract the commit hash for various errors, we only log a warning,
// and wait for the next hydration loop to retry the hydration.
func doneCommit(donePath string) string {
	commit, err := RenderedCommit(donePath)
	if err != nil {
		klog.Warningf("unable to read the done file %s: %v", donePath, err)
	} else if commit == "" {
		klog.Warningf("unable to check the status of the done file %s: %v", donePath, err)
	}
	return commit
}

// RenderedCommit extracts the commit hash from the done file if exists.
// Returns empty string with nil error if the done file does not exist.
func RenderedCommit(donePath string) (string, error) {
	commit, err := os.ReadFile(donePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Rendering in-progress
			return "", nil
		}
		return "", fmt.Errorf("reading rendering done file: %s", donePath)
	}
	return string(commit), nil
}

// exportError writes the error content to the error file.
func exportError(commit, root, errorFile string, hydrationError HydrationError) error {
	klog.Errorf("rendering error for commit %s: %v", commit, hydrationError)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		fileMode := os.FileMode(0755)
		if err := os.Mkdir(root, fileMode); err != nil {
			return fmt.Errorf("unable to create the root directory: %s: %w", root, err)
		}
	}

	tmpFile, err := os.CreateTemp(root, "tmp-err-")
	if err != nil {
		return fmt.Errorf("unable to create temporary error-file under directory %s: %w", root, err)
	}
	defer func() {
		if err := tmpFile.Close(); err != nil {
			klog.Warningf("unable to close temporary error-file: %s", tmpFile.Name())
		}
	}()

	payload := HydrationErrorPayload{
		Code:  hydrationError.Code(),
		Error: hydrationError.Error(),
	}

	jb, err := json.Marshal(payload)
	if err != nil {
		klog.Errorf("can't encode hydration error payload: %v", err)
		return err
	}

	if _, err = tmpFile.Write(jb); err != nil {
		return fmt.Errorf("unable to write to temporary error-file: %s: %w", tmpFile.Name(), err)
	}
	if err := os.Rename(tmpFile.Name(), errorFile); err != nil {
		return fmt.Errorf("unable to rename %s to %s: %w", tmpFile.Name(), errorFile, err)
	}
	if err := os.Chmod(errorFile, 0644); err != nil {
		return fmt.Errorf("unable to change permissions on the error-file: %s: %w", errorFile, err)
	}
	klog.Infof("Saved the rendering error in file: %s", errorFile)
	return nil
}

// deleteErrorFile deletes the error file.
func deleteErrorFile(file string) error {
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("unable to delete error file: %s: %w", file, err)
	}
	return nil
}

// SourceCommitAndSyncPathWithRetry returns the source hash (git commit hash,
// OCI image digest, or helm chart version), the absolute path of the sync
// directory, and any source errors.
// It retries with the provided backoff.
func SourceCommitAndSyncPathWithRetry(backoff wait.Backoff, sourceType configsync.SourceType, sourcePath cmpath.Absolute, syncDir cmpath.Relative, reconcilerName string) (commit string, syncPath cmpath.Absolute, _ status.Error) {
	err := util.RetryWithBackoff(backoff, func() error {
		var err error
		commit, syncPath, err = SourceCommitAndSyncPath(sourceType, sourcePath, syncDir, reconcilerName)
		return err
	})
	// If a retriable error can't be addressed with retry, it is identified as a
	// source error, and will be exposed in the R*Sync status.
	return commit, syncPath, status.SourceError.Wrap(err).Build()
}

// SourceCommitAndSyncPath returns the source hash (git commit hash, OCI image
// digest, or helm chart version), the absolute path of the sync directory,
// and any source errors.
func SourceCommitAndSyncPath(sourceType configsync.SourceType, sourcePath cmpath.Absolute, syncDir cmpath.Relative, reconcilerName string) (string, cmpath.Absolute, error) {
	sourceRoot := path.Dir(sourcePath.OSPath())
	if _, err := os.Stat(sourceRoot); err != nil {
		// It fails to check the source root directory status, either because of
		// the path doesn't exist, or other OS failures. The root cause is
		// probably because the *-sync container is not ready yet, so retry until
		// it becomes ready.
		return "", "", util.NewRetriableError(fmt.Errorf("failed to check the status of the source root directory %q: %w", sourceRoot, err))
	}
	// Check if the source configs are pulled successfully.
	errFilePath := filepath.Join(sourceRoot, ErrorFile)

	var containerName string
	switch sourceType {
	case configsync.OciSource:
		containerName = reconcilermanager.OciSync
	case configsync.GitSource:
		containerName = reconcilermanager.GitSync
	case configsync.HelmSource:
		containerName = reconcilermanager.HelmSync
	}

	content, err := os.ReadFile(errFilePath)
	switch {
	case err != nil && !os.IsNotExist(err):
		// It fails to get the status of the source error file, probably because
		// the *-sync container is not ready yet, so retry until it becomes ready.
		return "", "", util.NewRetriableError(fmt.Errorf("failed to check the status of the source error file %q: %v", errFilePath, err))
	case err == nil && len(content) == 0:
		// The source error file exists, which indicates the *-sync container is
		// ready. The file is empty probably because *-sync fails to write to the
		// error file. Hence, no need to retry.
		return "", "", fmt.Errorf("%s is empty. Please check %s logs for more info: kubectl logs -n %s -l %s -c %s",
			errFilePath, containerName, configsync.ControllerNamespace,
			metadata.ReconcilerLabel, reconcilerName)
	case err == nil && len(content) != 0:
		// The source error file exists, which indicates the *-sync container is
		// ready, so return the error directly without retry.
		return "", "", fmt.Errorf("error in the %s container: %s", containerName, string(content))
	default:
		// The sourceRoot directory exists, but the source error file doesn't exist.
		// It indicates that *-sync is ready, but no errors so far.
	}

	gitDir, err := sourcePath.EvalSymlinks()
	if err != nil {
		// `sourcePath` points to the directory with commit SHA that holds the
		// checked-out files. It can't be evaluated probably because *-sync
		// container is ready, but hasn't finished creating the symlink yet, so
		// retry until the symlink is created.
		return "", "", util.NewRetriableError(fmt.Errorf("failed to evaluate the source rev directory %q: %v", sourcePath, err))
	}

	commit := filepath.Base(gitDir.OSPath())

	// The hydration controller might pull remote Helm charts locally, which makes the source directory dirty.
	// Hence, we don't check if the source directory is clean before the hydration.
	// The assumption is that customers should have limited access to manually modify the source configs.
	// For the local Helm charts pulled by the Helm inflator, the entire hydrated directory
	// will blow away when new commits come in.
	// If the commit hash is not changed, the hydration will be skipped.
	// Therefore, it is relatively safe to keep the Helm charts local in the source directory.
	logicalSyncPath := gitDir.Join(syncDir)
	// Evaluate symlinks to get the physical path to the sync directory.
	// This is analogous to `pwd -P <path>`.
	physicalSyncPath, err := logicalSyncPath.EvalSymlinks()
	if err != nil {
		// `syncPath` points to the source config directory. Now the commit
		// SHA has been checked out, but it fails to evaluate the symlink with
		// the config directory, which indicates a misconfiguration of
		// `spec.git.dir`, `spec.oci.dir`, or `spec.helm.dir`, so return the
		// error without retry.
		return "", "", fmt.Errorf("failed to evaluate the config directory %q: %w", logicalSyncPath.OSPath(), err)
	}
	return commit, physicalSyncPath, nil
}
