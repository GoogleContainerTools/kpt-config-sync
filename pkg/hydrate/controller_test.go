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
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
	"kpt.dev/configsync/pkg/testing/testerrors"
)

const (
	originCommit    = "1234567890abcdef"
	differentCommit = "abcdef1234567890"
	// kustomization is a minimal kustomization.yaml file that passes validation
	kustomization = "namespace: test-ns"
)

func TestSourceCommitAndDirWithRetry(t *testing.T) {
	commit := "abcd123"
	syncDir := "configs"
	testCases := []struct {
		name                 string
		retryCap             time.Duration
		srcRootCreateLatency time.Duration
		symlinkCreateLatency time.Duration
		syncDir              string
		errFileExists        bool
		errFileContent       string
		expectedSourceCommit string
		expectedErrMsg       string
	}{
		{
			name:                 "source root directory isn't created within the retry cap",
			retryCap:             5 * time.Millisecond,
			srcRootCreateLatency: 10 * time.Millisecond,
			expectedErrMsg:       "failed to check the status of the source root directory",
		},
		{
			name:                 "source root directory created within the retry cap",
			retryCap:             100 * time.Millisecond,
			srcRootCreateLatency: 5 * time.Millisecond,
			expectedSourceCommit: commit,
		},
		{
			name:                 "symlink isn't created within the retry cap",
			retryCap:             5 * time.Millisecond,
			symlinkCreateLatency: 10 * time.Millisecond,
			expectedErrMsg:       "failed to evaluate the source rev directory",
		},
		{
			name:                 "symlink created within the retry cap",
			retryCap:             100 * time.Millisecond,
			symlinkCreateLatency: 5 * time.Millisecond,
			expectedSourceCommit: commit,
		},
		{
			name:           "error file exists with empty content",
			retryCap:       100 * time.Millisecond,
			errFileExists:  true,
			expectedErrMsg: "is empty. Please check git-sync logs for more info",
		},
		{
			name:           "error file exists with non-empty content",
			retryCap:       100 * time.Millisecond,
			errFileExists:  true,
			errFileContent: "git-sync error",
			expectedErrMsg: "git-sync error",
		},
		{
			name:           "sync directory doesn't exist",
			retryCap:       100 * time.Millisecond,
			syncDir:        "unknown",
			expectedErrMsg: "failed to evaluate the config directory",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			backoff := wait.Backoff{
				Duration: time.Millisecond,
				Factor:   2,
				Steps:    10,
				Cap:      tc.retryCap,
				Jitter:   0.1,
			}

			srcRootDir := filepath.Join(os.TempDir(), "test-srcCommit-syncDir", rand.String(10))
			commitDir := filepath.Join(srcRootDir, commit)
			if len(tc.syncDir) == 0 {
				tc.syncDir = syncDir
			}
			t.Cleanup(func() {
				if err := os.RemoveAll(srcRootDir); err != nil {
					t.Errorf("failed to remove source root directory: %v", err)
				}
			})

			// Simulating the creation of source configs and errors in the background
			doneCh := make(chan struct{})
			go func() {
				defer close(doneCh)
				err := func() error {
					srcRootDirCreated := false
					// Create the source root directory conditionally with latency
					if tc.srcRootCreateLatency > 0 {
						t.Logf("sleeping for %q before creating source root directory %q", tc.srcRootCreateLatency, srcRootDir)
						time.Sleep(tc.srcRootCreateLatency)
					}
					if tc.srcRootCreateLatency <= tc.retryCap {
						if err := os.MkdirAll(srcRootDir, 0700); err != nil {
							return fmt.Errorf("failed to create source root directory %q: %v", srcRootDir, err)
						}
						srcRootDirCreated = true
						t.Logf("source root directory %q created at %v", srcRootDir, time.Now())
					}

					// Create the error file based on the test condition
					if srcRootDirCreated && tc.errFileExists {
						errFilePath := filepath.Join(srcRootDir, ErrorFile)
						if err := os.WriteFile(errFilePath, []byte(tc.errFileContent), 0644); err != nil {
							return fmt.Errorf("failed to write to error file %q: %v", errFilePath, err)
						}
					}

					// Create the symlink conditionally with latency
					if tc.symlinkCreateLatency > 0 {
						t.Logf("sleeping for %q before creating the symlink %q", tc.symlinkCreateLatency, srcRootDir)
						time.Sleep(tc.symlinkCreateLatency)
					}
					if srcRootDirCreated && tc.symlinkCreateLatency <= tc.retryCap {
						if err := os.Mkdir(commitDir, os.ModePerm); err != nil {
							return fmt.Errorf("failed to create the commit directory %q: %v", commitDir, err)
						}
						// Create the sync directory if syncDir is the same as tc.syncDir
						if tc.syncDir == syncDir {
							syncDirPath := filepath.Join(commitDir, syncDir)
							if err := os.Mkdir(syncDirPath, os.ModePerm); err != nil {
								return fmt.Errorf("failed to create the sync directory %q: %v", syncDirPath, err)
							}
							t.Logf("sync directory %q created at %v", syncDirPath, time.Now())
						}
						symDir := filepath.Join(srcRootDir, "rev")
						if err := os.Symlink(commitDir, symDir); err != nil {
							return fmt.Errorf("failed to create the symlink %q: %v", symDir, err)
						}
						t.Logf("symlink %q created and linked to %q at %v", symDir, commitDir, time.Now())
					}
					return nil
				}()
				if err != nil {
					t.Log(err)
				}
			}()

			t.Logf("start calling SourceCommitAndDirWithRetry at %v", time.Now())
			srcCommit, srcSyncDir, err := SourceCommitAndSyncPathWithRetry(backoff, configsync.GitSource, cmpath.Absolute(commitDir), cmpath.RelativeOS(tc.syncDir), "root-reconciler")
			if tc.expectedErrMsg == "" {
				assert.Nil(t, err, "got unexpected error %v", err)
				assert.Equal(t, tc.expectedSourceCommit, srcCommit)
				assert.Equal(t, filepath.Join(commitDir, syncDir), srcSyncDir.OSPath())
			} else {
				assert.NotNil(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrMsg)
			}

			// Block and wait for the goroutine to complete.
			<-doneCh
		})
	}

}

func TestRunHydrate(t *testing.T) {
	testCases := []struct {
		name      string
		commit    string // the commit in the hydrator object that might differ from origin commit
		wantedErr error
	}{
		{
			name:      "Run hydrate when source commit is not changed",
			commit:    originCommit,
			wantedErr: nil,
		},
		{
			name:      "Run hydrate when source commit is changed",
			commit:    differentCommit,
			wantedErr: NewTransientError(fmt.Errorf("source commit changed while running Kustomize build, was %s, now %s. It will be retried in the next sync", originCommit, differentCommit)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// create a temporary directory with a commit hash
			tempDir, err := os.MkdirTemp(os.TempDir(), "run-hydrate-test")
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				err := os.RemoveAll(tempDir)
				if err != nil {
					t.Fatal(err)
				}
			}()

			commitDir := filepath.Join(tempDir, tc.commit)
			err = os.Mkdir(commitDir, os.ModePerm)
			if err != nil {
				t.Fatal(err)
			}
			err = os.WriteFile(filepath.Join(commitDir, "kustomization.yaml"), []byte(kustomization), 0666)
			if err != nil {
				t.Fatal(err)
			}

			// create a symlink to point to the temporary directory
			dir := ft.NewTestDir(t)
			symDir := dir.Root().Join(cmpath.RelativeSlash("run-hydrate-symlink"))
			err = os.Symlink(commitDir, symDir.OSPath())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				err := os.Remove(symDir.OSPath())
				if err != nil {
					t.Fatal(err)
				}
			}()

			hydrator := &Hydrator{
				DonePath:        "",
				SourceType:      configsync.HelmSource,
				SourceRoot:      cmpath.Absolute(commitDir),
				HydratedRoot:    cmpath.Absolute(commitDir),
				SourceLink:      "",
				HydratedLink:    "tmp-link",
				SyncDir:         "",
				PollingPeriod:   1 * time.Minute,
				RehydratePeriod: 1 * time.Minute,
				ReconcilerName:  "root-reconciler",
			}

			absSourceDir := hydrator.SourceRoot.Join(cmpath.RelativeSlash(hydrator.SourceLink))
			_, syncDir, err := SourceCommitAndSyncPath(hydrator.SourceType, absSourceDir, hydrator.SyncDir, hydrator.ReconcilerName)
			if err != nil {
				t.Fatal(fmt.Errorf("failed to get commit and sync directory from the source directory %s: %v", commitDir, err))
			}

			err = hydrator.runHydrate(originCommit, syncDir)
			testerrors.AssertEqual(t, tc.wantedErr, err)
		})
	}
}

func TestComputeCommit(t *testing.T) {
	testCases := []struct {
		name         string
		sourceCommit string
	}{
		{
			name:         "Computed commit should be the same to the one given in sourceDir",
			sourceCommit: originCommit,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// create a temporary directory with a commit hash
			tempDir, err := os.MkdirTemp(os.TempDir(), "compute-commit-test")
			if err != nil {
				t.Fatal(err)
			}
			defer func(path string) {
				err := os.RemoveAll(path)
				if err != nil {
					t.Fatal(err)
				}
			}(tempDir)
			absTempDir := cmpath.Absolute(tempDir)

			commitDir := filepath.Join(tempDir, originCommit)
			err = os.Mkdir(commitDir, os.ModePerm)
			if err != nil {
				t.Fatal(err)
			}

			// create a symlink to point to the temporary directory
			dir := ft.NewTestDir(t)
			symDir := dir.Root().Join(cmpath.RelativeSlash("compute-commit-symlink"))
			err = os.Symlink(commitDir, symDir.OSPath())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				err := os.Remove(symDir.OSPath())
				if err != nil {
					t.Fatal(err)
				}
			}()

			absSourceDir := absTempDir.Join(cmpath.RelativeSlash(tc.sourceCommit))
			computed, err := ComputeCommit(symDir)
			if computed != tc.sourceCommit {
				t.Errorf("wanted commit to be %v, got %v", tc.sourceCommit, computed)
			} else if err != nil {
				t.Errorf("error computing commit from %s: %v ", absSourceDir, err)
			}
		})
	}
}
