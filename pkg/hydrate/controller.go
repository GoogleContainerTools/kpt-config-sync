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
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
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
	SourceType v1beta1.SourceType
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
	absSourceDir := h.absSourceDir()
	var hydrateErr HydrationError
	var srcCommit string
	var syncDir cmpath.Absolute
	var err error
	for {
		select {
		case <-ctx.Done():
			return
		case <-rehydrateTimer.C:
			hydrateErr = h.rehydrateOnError(hydrateErr, srcCommit, syncDir)
			rehydrateTimer.Reset(h.RehydratePeriod) // Schedule rehydrate attempt
		case <-runTimer.C:
			// pull the source commit and directory with retries within 5 minutes.
			srcCommit, syncDir, err = SourceCommitAndDirWithRetry(util.SourceRetryBackoff, h.SourceType, absSourceDir, h.SyncDir, h.ReconcilerName)
			if err != nil {
				hydrateErr = NewInternalError(errors.Wrapf(err,
					"failed to get the commit hash and sync directory from the source directory %s",
					absSourceDir.OSPath()))
				if err := h.complete(srcCommit, hydrateErr); err != nil {
					klog.Errorf("failed to complete the rendering execution for commit %q: %v",
						srcCommit, err)
				}
			} else if DoneCommit(h.DonePath.OSPath()) != srcCommit {
				// If the commit has been processed before, regardless of success or failure,
				// skip the hydration to avoid repeated execution.
				// The rehydrate ticker will retry on the failed commit.
				hydrateErr = h.hydrate(srcCommit, syncDir)
				if err := h.complete(srcCommit, hydrateErr); err != nil {
					klog.Errorf("failed to complete the rendering execution for commit %q: %v", srcCommit, err)
				}
			}
			runTimer.Reset(h.PollingPeriod) // Schedule re-run attempt
		}
	}
}

// runHydrate runs `kustomize build` on the source configs.
func (h *Hydrator) runHydrate(sourceCommit string, syncDir cmpath.Absolute) HydrationError {
	newHydratedDir := h.HydratedRoot.Join(cmpath.RelativeOS(sourceCommit))
	dest := newHydratedDir.Join(h.SyncDir).OSPath()

	if err := kustomizeBuild(syncDir.OSPath(), dest, true); err != nil {
		return err
	}

	newCommit, err := ComputeCommit(h.absSourceDir())
	if err != nil {
		return NewTransientError(err)
	} else if sourceCommit != newCommit {
		return NewTransientError(fmt.Errorf("source commit changed while running Kustomize build, was %s, now %s. It will be retried in the next sync", sourceCommit, newCommit))
	}

	if err := updateSymlink(h.HydratedRoot.OSPath(), h.HydratedLink, newHydratedDir.OSPath()); err != nil {
		return NewInternalError(errors.Wrapf(err, "unable to update the symbolic link to %s", newHydratedDir.OSPath()))
	}
	klog.Infof("Successfully rendered %s for commit %s", syncDir.OSPath(), sourceCommit)
	return nil
}

// ComputeCommit returns the computed commit from given sourceDir, or error
// if the sourceDir fails symbolic link evaluation
func ComputeCommit(sourceDir cmpath.Absolute) (string, error) {
	dir, err := sourceDir.EvalSymlinks()
	if err != nil {
		return "", errors.Wrapf(err, "unable to evaluate the symbolic link of sourceDir %s", dir)
	}
	newCommit := filepath.Base(dir.OSPath())
	return newCommit, nil
}

// absSourceDir returns the absolute path of a source directory by joining the
// root source directory path and a relative path to the source directory
func (h *Hydrator) absSourceDir() cmpath.Absolute {
	return h.SourceRoot.Join(cmpath.RelativeSlash(h.SourceLink))
}

// hydrate renders the source git repo to hydrated configs.
func (h *Hydrator) hydrate(sourceCommit string, syncDirPath cmpath.Absolute) HydrationError {
	syncDir := syncDirPath.OSPath()
	hydrate, err := needsKustomize(syncDir)
	if err != nil {
		return NewInternalError(errors.Wrapf(err, "unable to check if rendering is needed for the source directory: %s", syncDir))
	}
	if !hydrate {
		found, err := hasKustomizeSubdir(syncDir)
		if err != nil {
			return NewInternalError(err)
		}
		if found {
			return NewActionableError(errors.Errorf("Kustomization config file is missing from the sync directory %s. "+
				"To fix, either add kustomization.yaml in the sync directory to trigger the rendering process, "+
				"or remove kustomizaiton.yaml from all sub directories to skip rendering.", syncDir))
		}
		klog.V(5).Infof("no rendering is needed because of no Kustomization config file in the source configs with commit %s", sourceCommit)
		if err := os.RemoveAll(h.HydratedRoot.OSPath()); err != nil {
			return NewInternalError(err)
		}
		return nil
	}

	// Remove the done file because a new hydration is in progress.
	if err := os.RemoveAll(h.DonePath.OSPath()); err != nil {
		return NewInternalError(errors.Wrapf(err, "unable to remove the done file: %s", h.DonePath.OSPath()))
	}
	return h.runHydrate(sourceCommit, syncDirPath)
}

// rehydrateOnError is triggered by the rehydrateTimer (every 30 mins)
// It re-runs the rendering process when there is a previous error.
func (h *Hydrator) rehydrateOnError(prevErr HydrationError, prevSrcCommit string, prevSyncDir cmpath.Absolute) HydrationError {
	if prevErr == nil {
		// Return directly if the previous hydration succeeded.
		return nil
	}
	if len(prevSrcCommit) == 0 || len(prevSyncDir) == 0 {
		// If source commit and directory isn't available, skip rehydrating because
		// rehydrateOnError is supposed to run after h.hydrate processes the commit.
		return prevErr
	}
	klog.Infof("retry rendering commit %s", prevSrcCommit)
	hydrationErr := h.runHydrate(prevSrcCommit, prevSyncDir)
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
			return errors.Wrapf(err, "unable to access the current hydrated directory: %s", linkPath)
		}
		deleteOldDir = false
	}

	if err := os.Symlink(newDir, tmpLinkPath); err != nil {
		return errors.Wrap(err, "unable to create symlink")
	}

	if err := os.Rename(tmpLinkPath, linkPath); err != nil {
		return errors.Wrap(err, "unable to replace symlink")
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
		return errors.Wrapf(err, "unable to create done file: %s", h.DonePath.OSPath())
	}
	if _, err = done.WriteString(commit); err != nil {
		return errors.Wrapf(err, "unable to write to commit hash to the done file: %s", h.DonePath)
	}
	if err := done.Close(); err != nil {
		klog.Warningf("unable to close the done file %s: %v", h.DonePath.OSPath(), err)
	}
	return nil
}

// DoneCommit extracts the commit hash from the done file if exists.
// It returns the commit hash if exists, otherwise, returns an empty string.
// If it fails to extract the commit hash for various errors, we only log a warning,
// and wait for the next hydration loop to retry the hydration.
func DoneCommit(donePath string) string {
	if _, err := os.Stat(donePath); err == nil {
		commit, err := ioutil.ReadFile(donePath)
		if err != nil {
			klog.Warningf("unable to read the done file %s: %v", donePath, err)
			return ""
		}
		return string(commit)
	} else if !os.IsNotExist(err) {
		klog.Warningf("unable to check the status of the done file %s: %v", donePath, err)
	}
	return ""
}

// exportError writes the error content to the error file.
func exportError(commit, root, errorFile string, hydrationError HydrationError) error {
	klog.Errorf("rendering error for commit %s: %v", commit, hydrationError)
	if _, err := os.Stat(root); os.IsNotExist(err) {
		fileMode := os.FileMode(0755)
		if err := os.Mkdir(root, fileMode); err != nil {
			return errors.Wrapf(err, "unable to create the root directory: %s", root)
		}
	}

	tmpFile, err := ioutil.TempFile(root, "tmp-err-")
	if err != nil {
		return errors.Wrapf(err, "unable to create temporary error-file under directory %s", root)
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
		return errors.Wrapf(err, "unable to write to temporary error-file: %s", tmpFile.Name())
	}
	if err := os.Rename(tmpFile.Name(), errorFile); err != nil {
		return errors.Wrapf(err, "unable to rename %s to %s", tmpFile.Name(), errorFile)
	}
	if err := os.Chmod(errorFile, 0644); err != nil {
		return errors.Wrapf(err, "unable to change permissions on the error-file: %s", errorFile)
	}
	klog.Infof("Saved the rendering error in file: %s", errorFile)
	return nil
}

// deleteErrorFile deletes the error file.
func deleteErrorFile(file string) error {
	if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "unable to delete error file: %s", file)
	}
	return nil
}

// SourceCommitAndDirWithRetry returns the source hash (a git commit hash or an
// OCI image digest or a helm chart version), the absolute path of the sync
// directory, and source errors.
// It retries with the provided backoff.
func SourceCommitAndDirWithRetry(backoff wait.Backoff, sourceType v1beta1.SourceType, sourceRevDir cmpath.Absolute, syncDir cmpath.Relative, reconcilerName string) (commit string, sourceDir cmpath.Absolute, _ status.Error) {
	err := util.RetryWithBackoff(backoff, func() error {
		var err error
		commit, sourceDir, err = SourceCommitAndDir(sourceType, sourceRevDir, syncDir, reconcilerName)
		return err
	})
	// If a retriable error can't be addressed with retry, it is identified as a
	// source error, and will be exposed in the R*Sync status.
	return commit, sourceDir, status.SourceError.Wrap(err).Build()
}

// SourceCommitAndDir returns the source hash (a git commit hash or an OCI image
// digest or a helm chart version), the absolute path of the sync directory,
// and source errors.
func SourceCommitAndDir(sourceType v1beta1.SourceType, sourceRevDir cmpath.Absolute, syncDir cmpath.Relative, reconcilerName string) (string, cmpath.Absolute, error) {
	sourceRoot := path.Dir(sourceRevDir.OSPath())
	if _, err := os.Stat(sourceRoot); err != nil {
		// It fails to check the source root directory status, either because of
		// the path doesn't exist, or other OS failures. The root cause is
		// probably because the *-sync container is not ready yet, so retry until
		// it becomes ready.
		return "", "", util.NewRetriableError(fmt.Errorf("failed to check the status of the source root directory %q: %v", sourceRoot, err))
	}
	// Check if the source configs are pulled successfully.
	errFilePath := filepath.Join(sourceRoot, ErrorFile)

	var containerName string
	switch sourceType {
	case v1beta1.OciSource:
		containerName = reconcilermanager.OciSync
	case v1beta1.GitSource:
		containerName = reconcilermanager.GitSync
	case v1beta1.HelmSource:
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

	gitDir, err := sourceRevDir.EvalSymlinks()
	if err != nil {
		// `sourceRevDir` points to the directory with commit SHA that holds the
		// checked-out files. It can't be evaluated probably because *-sync
		// container is ready, but hasn't finished creating the symlink yet, so
		// retry until the symlink is created.
		return "", "", util.NewRetriableError(fmt.Errorf("failed to evaluate the source rev directory %q: %v", sourceRevDir, err))
	}

	commit := filepath.Base(gitDir.OSPath())

	// The hydration controller might pull remote Helm charts locally, which makes the source directory dirty.
	// Hence, we don't check if the source directory is clean before the hydration.
	// The assumption is that customers should have limited access to manually modify the source configs.
	// For the local Helm charts pulled by the Helm inflator, the entire hydrated directory
	// will blow away when new commits come in.
	// If the commit hash is not changed, the hydration will be skipped.
	// Therefore, it is relatively safe to keep the Helm charts local in the source directory.
	relSyncDir := gitDir.Join(syncDir)
	sourceDir, err := relSyncDir.EvalSymlinks()
	if err != nil {
		// `relSyncDir` points to the source config directory. Now the commit SHA
		// has been checked out, but it fails to evaluate the symlink with the
		// config directory, which indicates a misconfiguration of `spec.git.dir`,
		// `spec.oci.dir`, or `spec.helm.dir`, so return the error without retry.
		return "", "", fmt.Errorf("failed to evaluate the config directory %q: %w", relSyncDir.OSPath(), err)
	}
	return commit, sourceDir, nil
}
