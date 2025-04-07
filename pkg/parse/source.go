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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/util/wait"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util"
)

// FileSource includes all settings to configure where a Parser reads files from.
type FileSource struct {
	// SourceDir is the path to the symbolic link of the source repository.
	SourceDir cmpath.Absolute
	// HydratedRoot is the path to the root of the hydrated directory.
	HydratedRoot string
	// RepoRoot is the absolute path to the parent directory of SourceRoot and HydratedRoot.
	RepoRoot cmpath.Absolute
	// HydratedLink is the relative path to the symbolic link of the hydrated configs.
	HydratedLink string
	// SyncDir is the path to the directory of policies within the source repository.
	SyncDir cmpath.Relative
	// SourceType is the type of the source repository, must be git or oci.
	SourceType configsync.SourceType
	// SourceRepo is the source repo to sync.
	SourceRepo string
	// SourceBranch is the branch of the source repo to sync.
	SourceBranch string
	// SourceRev is the revision of the source repo to sync.
	SourceRev            string
	ReconcilerSignalsDir cmpath.Absolute
}

// Files lists files in a repository and ensures the source repository hasn't been
// modified from HEAD.
type Files struct {
	// TODO: unwrap composition
	FileSource
}

// sourceState contains all state read from the mounted source repo.
type sourceState struct {
	// spec is the source specification as read from the FileSource.
	// This cache avoids re-generating the spec every time the status is updated.
	spec SourceSpec
	// commit is the commit read from the source of truth.
	commit string
	// syncPath is the absolute path to the sync directory that includes the configurations.
	syncPath cmpath.Absolute
	// files is the list of all observed files in the sync directory (recursively).
	files []cmpath.Absolute
}

// readConfigFiles reads all the files under state.syncPath and sets state.files.
// - if rendering is enabled, state.syncPath contains the hydrated files.
// - if rendered is disabled, state.syncPath contains the source files.
// readConfigFiles should be called after sourceState is populated.
func (o *Files) readConfigFiles(state *sourceState) status.Error {
	if state == nil || state.commit == "" || state.syncPath == "" {
		return status.InternalError("sourceState is not populated yet")
	}
	syncPath := state.syncPath
	fileList, err := listFiles(syncPath, map[string]bool{".git": true})
	if err != nil {
		return status.PathWrapError(fmt.Errorf("listing files in the configs directory: %w", err), syncPath.OSPath())
	}

	newCommit, err := hydrate.ComputeCommit(o.SourceDir)
	if err != nil {
		return status.TransientError(err)
	} else if newCommit != state.commit {
		return status.TransientError(fmt.Errorf("source commit changed while listing files, was %s, now %s. It will be retried in the next sync", state.commit, newCommit))
	}

	state.files = fileList
	return nil
}

func (o *Files) sourceContext() sourceContext {
	return sourceContext{
		Repo:   o.SourceRepo,
		Branch: o.SourceBranch,
		Rev:    o.SourceRev,
	}
}

// readHydratedPathWithRetry returns a sourceState object whose `commit` and `syncPath` fields are set if succeeded with retries.
func (o *Files) readHydratedPathWithRetry(backoff wait.Backoff, hydratedRoot cmpath.Absolute, reconciler string, srcState *sourceState) (*sourceState, hydrate.HydrationError) {
	result := &sourceState{
		spec: srcState.spec,
	}
	err := util.RetryWithBackoff(backoff, func() error {
		var err error
		result, err = o.readHydratedPath(hydratedRoot, reconciler, srcState)
		return err
	})
	if err == nil {
		return result, nil
	}
	hydrationErr, ok := err.(hydrate.HydrationError)
	if ok {
		return result, hydrationErr
	}
	return result, hydrate.NewInternalError(err)
}

// readHydratedPath returns a sourceState object whose `commit` and `syncPath` fields are set if succeeded.
func (o *Files) readHydratedPath(hydratedRoot cmpath.Absolute, reconciler string, srcState *sourceState) (*sourceState, error) {
	result := &sourceState{
		spec: srcState.spec,
	}
	errorFile := hydratedRoot.Join(cmpath.RelativeSlash(hydrate.ErrorFile))
	_, err := os.Stat(errorFile.OSPath())
	switch {
	case err == nil:
		// hydration error file exist, return the error
		return result, hydratedError(errorFile.OSPath(),
			fmt.Sprintf("%s=%s", metadata.ReconcilerLabel, reconciler))
	case !os.IsNotExist(err):
		// failed to check the hydration error file, retry
		return result, util.NewRetriableError(fmt.Errorf("failed to check the error file %s: %v", errorFile.OSPath(), err))
	default:
		// the hydration error file doesn't exist
		logicalHydratedPath := hydratedRoot.Join(cmpath.RelativeSlash(o.HydratedLink))
		physicalHydratedPath, err := logicalHydratedPath.EvalSymlinks()
		if err != nil {
			// Retry if failed to load the hydrated directory
			return result, util.NewRetriableError(fmt.Errorf("failed to load the hydrated configs under %s", hydratedRoot.OSPath()))
		}
		result.commit = filepath.Base(physicalHydratedPath.OSPath())
		if result.commit != srcState.commit {
			// It is not always retriable locally, so return a transient error for the reconciler's retryTime to trigger a retry.
			// - If the source commit is newer than the hydrated commit, it is
			//   retriable because the hydrated commit will be re-evaluated.
			// - If the hydrated commit is newer than the source commit, retry won't
			//   help because srcState.commit remains unchanged.
			return result, hydrate.NewTransientError(fmt.Errorf("source commit changed while listing hydrated files, was %s, now %s. It will be retried in the next sync", srcState.commit, result.commit))
		}

		logicalSyncPath := physicalHydratedPath.Join(o.SyncDir)
		// Evaluate symlinks to get the physical path to the sync directory.
		// This is analogous to `pwd -P <path>`.
		physicalSyncPath, err := logicalSyncPath.EvalSymlinks()
		if err != nil {
			// Retry if the symlink failed to be evaluated.
			return result, util.NewRetriableError(fmt.Errorf("failed to evaluate symbolic link to the hydrated sync directory %s: %v", logicalSyncPath.OSPath(), err))
		}
		result.syncPath = physicalSyncPath
	}
	return result, nil
}

// listFiles returns a list of all files in the specified directory.
func listFiles(dir cmpath.Absolute, ignore map[string]bool) ([]cmpath.Absolute, error) {
	var result []cmpath.Absolute
	err := filepath.Walk(dir.OSPath(),
		func(path string, fi os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if fi.IsDir() {
				if _, contains := ignore[fi.Name()]; contains {
					return filepath.SkipDir
				}
				return nil
			}
			abs, err := cmpath.AbsoluteOS(path)
			if err != nil {
				return err
			}
			abs, err = abs.EvalSymlinks()
			if err != nil {
				return err
			}
			result = append(result, abs)
			return nil
		})
	return result, err
}

// hydratedError returns the error details from the error file generated by the hydration controller.
func hydratedError(errorFile, label string) hydrate.HydrationError {
	content, err := os.ReadFile(errorFile)
	if err != nil {
		return hydrate.NewInternalError(fmt.Errorf("unable to load %s: %v. Please check %s logs for more info: kubectl logs -n %s -l %s -c %s",
			errorFile, err, reconcilermanager.HydrationController, configsync.ControllerNamespace, label, reconcilermanager.HydrationController))
	}
	if len(content) == 0 {
		return hydrate.NewInternalError(fmt.Errorf("%s is empty. Please check %s logs for more info: kubectl logs -n %s -l %s -c %s",
			errorFile, reconcilermanager.HydrationController, configsync.ControllerNamespace, label, reconcilermanager.HydrationController))
	}

	payload := &hydrate.HydrationErrorPayload{}
	if err := json.Unmarshal(content, payload); err != nil {
		return hydrate.NewInternalError(err)
	}
	if payload.Code == status.ActionableHydrationErrorCode {
		return hydrate.NewActionableError(errors.New(payload.Error))
	}
	return hydrate.NewInternalError(errors.New(payload.Error))
}
