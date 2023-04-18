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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

var originCommit = "1234567890abcdef"
var differentCommit = "abcdef1234567890"

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
			wantedErr: testutil.EqualError(NewTransientError(fmt.Errorf("source commit changed while running Kustomize build, was %s, now %s. It will be retried in the next sync", originCommit, differentCommit))),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// create a temporary directory with a commit hash
			tempDir, err := ioutil.TempDir(os.TempDir(), "run-hydrate-test")
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

			kustFileGenerated, err := ioutil.TempFile(commitDir, "kustomization.yaml")
			if err != nil {
				t.Fatal(err)
			}
			err = os.Rename(kustFileGenerated.Name(), filepath.Join(commitDir, "kustomization.yaml"))
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
				SourceType:      v1beta1.HelmSource,
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
			_, syncDir, err := SourceCommitAndDir(hydrator.SourceType, absSourceDir, hydrator.SyncDir, hydrator.ReconcilerName)
			if err != nil {
				t.Fatal(fmt.Errorf("failed to get commit and sync directory from the source directory %s: %v", commitDir, err))
			}

			err = hydrator.runHydrate(originCommit, syncDir.OSPath())
			testutil.AssertEqual(t, tc.wantedErr, err)
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
			tempDir, err := ioutil.TempDir(os.TempDir(), "compute-commit-test")
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

func TestFilterArgsFromErrorPayload(t *testing.T) {
	testCases := []struct {
		description    string
		input          string
		expectedOutput string
	}{
		{
			description:    "non-json input should be unchanged",
			input:          "foo bar baz",
			expectedOutput: "foo bar baz",
		},
		{
			description:    "empty json input should be unchanged",
			input:          "{}",
			expectedOutput: "{}",
		},
		{
			description:    "json of different schema should be unchanged",
			input:          `{"foo":"bar"}`,
			expectedOutput: `{"foo":"bar"}`,
		},
		{
			description:    "ErrorPayload input without Args should be unchanged",
			input:          `{"Msg":"hello","Err":"some error"}`,
			expectedOutput: `{"Msg":"hello","Err":"some error"}`,
		},
		{
			description:    "ErrorPayload input with Args should remove Args",
			input:          `{"Msg":"hello","Err":"some error","Args":{"failCount":42,"waitTime":15000000000}}`,
			expectedOutput: `{"Msg":"hello","Err":"some error"}`,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			output := filterArgsFromErrorPayload([]byte(tc.input))
			require.Equal(t, tc.expectedOutput, string(output))
		})
	}
}
