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

package syncclient

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/wait"
	"kpt.dev/configsync/pkg/hydrate"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
	"kpt.dev/configsync/pkg/status"
)

const (
	symLink         = "rev"
	originCommit    = "1234567890abcde"
	differentCommit = "abcde1234567890"
)

func TestReadConfigFiles(t *testing.T) {
	testCases := []struct {
		name      string
		commit    string
		wantedErr error
	}{
		{
			name:      "read config files when commit is not changed",
			commit:    originCommit,
			wantedErr: nil,
		},
		{
			name:      "read config files when commit is changed",
			commit:    differentCommit,
			wantedErr: status.TransientError(fmt.Errorf("source commit changed while listing files, was %s, now %s. It will be retried in the next sync", originCommit, differentCommit)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// create temporary directory for parser
			tempRoot, _ := os.MkdirTemp(os.TempDir(), "read-config-test")
			t.Cleanup(func() {
				if err := os.RemoveAll(tempRoot); err != nil {
					t.Errorf("failed to tempRoot directory: %v", err)
				}
			})

			// mock the parser's syncPath that could change while program running
			parserCommitPath := filepath.Join(tempRoot, tc.commit)
			err := os.Mkdir(parserCommitPath, os.ModePerm)
			if err != nil {
				t.Fatal(err)
			}

			// mock the original sourceCommit that is passed by SourceState when
			// running ReadConfigFiles
			sourceCommitPath := filepath.Join(tempRoot, originCommit)
			if _, err := os.Stat(sourceCommitPath); errors.Is(err, os.ErrNotExist) {
				err = os.Mkdir(sourceCommitPath, os.ModePerm)
				if err != nil {
					t.Fatal(err)
				}
			}

			// create a symlink to point to the temporary directory
			dir := ft.NewTestDir(t)
			symDir := dir.Root().Join(cmpath.RelativeSlash("list-file-symlink"))
			err = os.Symlink(parserCommitPath, symDir.OSPath())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				err := os.Remove(symDir.OSPath())
				if err != nil {
					t.Fatal(err)
				}
			}()

			srcState := &SourceState{
				Spec:     nil, // TODO: Add tests for behavior when spec is non-nil
				Commit:   originCommit,
				SyncPath: cmpath.Absolute(sourceCommitPath),
				Files:    nil,
			}

			files := &Files{}

			// set the necessary FileSource of parser
			files.SourceDir = symDir

			err = files.ReadConfigFiles(srcState)
			assert.Equal(t, tc.wantedErr, err)
		})
	}
}

func TestReadHydratedPathWithRetry(t *testing.T) {
	syncDir := "configs"
	testCases := []struct {
		name                 string
		commit               string
		syncDir              string
		hydrationErr         string
		retryCap             time.Duration
		symlinkCreateLatency time.Duration
		expectedCommit       string
		expectedErrMsg       string
		expectedErrCode      string
	}{
		{
			name:           "read hydration status when commit is not changed",
			commit:         originCommit,
			expectedCommit: originCommit,
		},
		{
			name:            "read hydration status when commit is changed",
			commit:          differentCommit,
			expectedErrMsg:  fmt.Sprintf("source commit changed while listing hydrated files, was %s, now %s. It will be retried in the next sync", originCommit, differentCommit),
			expectedErrCode: status.TransientErrorCode,
		},
		{
			name:                 "symlink isn't created within the retry cap",
			retryCap:             5 * time.Millisecond,
			symlinkCreateLatency: 10 * time.Millisecond,
			expectedErrMsg:       "failed to load the hydrated configs under",
			expectedErrCode:      status.InternalHydrationErrorCode,
		},
		{
			name:                 "symlink created within the retry cap",
			commit:               originCommit,
			retryCap:             100 * time.Millisecond,
			symlinkCreateLatency: 5 * time.Millisecond,
			expectedCommit:       originCommit,
		},
		{
			name:            "error file exists",
			retryCap:        100 * time.Millisecond,
			hydrationErr:    `{"code": "1068", "error": "actionable-error"}`,
			expectedErrMsg:  "actionable-error",
			expectedErrCode: status.ActionableHydrationErrorCode,
		},
		{
			name:            "sync directory doesn't exist",
			retryCap:        100 * time.Millisecond,
			commit:          originCommit,
			syncDir:         "unknown",
			expectedErrMsg:  "failed to evaluate symbolic link to the hydrated sync directory",
			expectedErrCode: status.InternalHydrationErrorCode,
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

			if len(tc.syncDir) == 0 {
				tc.syncDir = syncDir
			}

			// create temporary directory for parser
			tempRoot, _ := os.MkdirTemp(os.TempDir(), "read-hydrated-dir-test")
			t.Cleanup(func() {
				if err := os.RemoveAll(tempRoot); err != nil {
					t.Error(err)
				}
			})
			hydratedRoot := filepath.Join(tempRoot, "hydrated")
			parserCommitPath := filepath.Join(hydratedRoot, tc.commit)
			if err := os.Mkdir(hydratedRoot, os.ModePerm); err != nil {
				t.Fatal(err)
			}

			// Simulating the creation of hydrated configs and errors in the background
			doneCh := make(chan struct{})
			go func() {
				defer close(doneCh)
				err := func() error {
					if len(tc.hydrationErr) != 0 {
						errFilePath := filepath.Join(hydratedRoot, hydrate.ErrorFile)
						if err := os.WriteFile(errFilePath, []byte(tc.hydrationErr), 0644); err != nil {
							return fmt.Errorf("failed to write to error file %q: %v", errFilePath, err)
						}
						// Skip creating symlink if it has a hydration error
						return nil
					}

					// Create the symlink conditionally with latency
					if tc.symlinkCreateLatency > 0 {
						t.Logf("sleeping for %q before creating the symlink %q", tc.symlinkCreateLatency, hydratedRoot)
						time.Sleep(tc.symlinkCreateLatency)
					}
					if tc.symlinkCreateLatency <= tc.retryCap {
						// mock the parser's syncPath that could change while program running
						if err := os.Mkdir(parserCommitPath, os.ModePerm); err != nil {
							return fmt.Errorf("failed to create the commit directory %q: %v", parserCommitPath, err)
						}

						// Create the sync directory
						if tc.syncDir == syncDir {
							syncPath := filepath.Join(parserCommitPath, syncDir)
							if err := os.Mkdir(syncPath, os.ModePerm); err != nil {
								return fmt.Errorf("failed to create the sync directory %q: %v", syncPath, err)
							}
							t.Logf("sync directory %q created at %v", syncPath, time.Now())
						}

						// create a symlink to point to the temporary directory
						logicalHydratedPath := filepath.Join(hydratedRoot, symLink)
						if err := os.Symlink(parserCommitPath, logicalHydratedPath); err != nil {
							return fmt.Errorf("failed to create the symlink %q: %v", logicalHydratedPath, err)
						}
						t.Logf("symlink %q created and linked to %q at %v", logicalHydratedPath, parserCommitPath, time.Now())
					}
					return nil
				}()
				if err != nil {
					t.Log(err)
				}
			}()

			srcState := &SourceState{
				Spec:   nil, // TODO: Add tests for behavior when spec is non-nil
				Commit: originCommit,
			}

			files := Files{
				FileSource: FileSource{
					HydratedRoot: hydratedRoot,
					HydratedLink: symLink,
					SyncDir:      cmpath.RelativeOS(tc.syncDir),
				},
			}

			wantState := &SourceState{
				Commit:   tc.commit,
				SyncPath: cmpath.Absolute(filepath.Join(parserCommitPath, syncDir)),
			}

			t.Logf("start calling readHydratedDirWithRetry at %v", time.Now())
			hydrationState, hydrationErr := files.ReadHydratedPathWithRetry(backoff,
				cmpath.Absolute(hydratedRoot), "unused", srcState)

			if tc.expectedErrMsg == "" {
				assert.Nil(t, hydrationErr)
				assert.Equal(t, wantState, hydrationState)
			} else {
				assert.NotNil(t, hydrationErr)
				assert.Contains(t, hydrationErr.Error(), tc.expectedErrMsg)
				assert.Equal(t, tc.expectedErrCode, hydrationErr.Code())
			}

			// Block and wait for the goroutine to complete.
			<-doneCh
		})
	}
}
