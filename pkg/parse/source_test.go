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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	syncertest "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

var originCommit = "1234567890abcde"
var differentCommit = "abcde1234567890"

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
			wantedErr: testutil.EqualError(status.TransientError(fmt.Errorf("source commit changed while running Kustomize build, was %s, now %s. It will be retried in the next sync", originCommit, differentCommit))),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// create temporary directory for parser
			tempRoot, _ := ioutil.TempDir(os.TempDir(), "read-config-test")
			defer func(path string) {
				err := os.RemoveAll(path)
				if err != nil {
					t.Fatal(err)
				}
			}(tempRoot)

			// mock the parser's syncDir that could change while program running
			parserCommitDir := filepath.Join(tempRoot, tc.commit)
			err := os.Mkdir(parserCommitDir, os.ModePerm)
			if err != nil {
				t.Fatal(err)
			}

			// mock the original sourceCommit that is passed by sourceState when
			// running readConfigFiles
			sourceCommitDir := filepath.Join(tempRoot, originCommit)
			if _, err := os.Stat(sourceCommitDir); errors.Is(err, os.ErrNotExist) {
				err = os.Mkdir(sourceCommitDir, os.ModePerm)
				if err != nil {
					t.Fatal(err)
				}
			}

			// create a symlink to point to the temporary directory
			dir := ft.NewTestDir(t)
			symDir := dir.Root().Join(cmpath.RelativeSlash("list-file-symlink"))
			err = os.Symlink(parserCommitDir, symDir.OSPath())
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				err := os.Remove(symDir.OSPath())
				if err != nil {
					t.Fatal(err)
				}
			}()

			sourceState := &sourceState{
				commit:  originCommit,
				syncDir: cmpath.Absolute(sourceCommitDir),
				files:   nil,
			}

			parser := &root{
				sourceFormat: filesystem.SourceFormatUnstructured,
				opts: opts{
					parser:             &fakeParser{},
					syncName:           rootSyncName,
					reconcilerName:     rootReconcilerName,
					client:             syncertest.NewClient(t, core.Scheme, fake.RootSyncObjectV1Beta1(rootSyncName)),
					discoveryInterface: syncertest.NewDiscoveryClient(kinds.Namespace(), kinds.Role()),
					updater: updater{
						scope:     declared.RootReconciler,
						resources: &declared.Resources{},
					},
					mux: &sync.Mutex{},
				},
			}

			// set the necessary FileSource of parser
			parser.SourceDir = symDir

			err = parser.readConfigFiles(sourceState, parser)
			testutil.AssertEqual(t, tc.wantedErr, err)
		})
	}
}
