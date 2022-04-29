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

package initialize

import (
	"os"
	"os/exec"
	"testing"

	"kpt.dev/configsync/cmd/nomos/flags"
	"kpt.dev/configsync/cmd/nomos/vet"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
)

func resetFlags() {
	// Flags are global state carried over between tests.
	// Cobra lazily evaluates flags only if they are declared, so unless these
	// are reset, successive calls to Cmd.Execute aren't guaranteed to be
	// independent.
	flags.Path = flags.PathDefault
	flags.SkipAPIServer = true
	forceValue = false
}

type testCase struct {
	name        string
	testDirOpts []ft.TestDirOpt
	args        []string
	wantError   bool
}

var gitInit ft.TestDirOpt = func(t *testing.T, testDir cmpath.Absolute) {
	output, err := exec.Command("git", "-C", testDir.OSPath(), "init").CombinedOutput()
	if err != nil {
		t.Log(output)
		t.Fatal(err)
	}
}

func TestNomosInitWorkingDirectory(t *testing.T) {
	resetFlags()

	testDir := ft.NewTestDir(t)

	err := os.Chdir(testDir.Root().OSPath())
	if err != nil {
		t.Fatal(err)
	}

	os.Args = []string{""}

	// Run command
	err = Cmd.Execute()
	if err != nil {
		t.Fatal(err)
	}

	// Ensure init-ed directory passes vet.
	err = vet.Cmd.Execute()
	if err != nil {
		t.Error(err)
	}
}

func TestNomosInit(t *testing.T) {
	testCases := []testCase{
		{
			name:      "empty dir",
			wantError: false,
		},
		{
			name: "empty dir with git",
			testDirOpts: []ft.TestDirOpt{
				gitInit,
			},
			wantError: false,
		},
		{
			name: "dir with file",
			testDirOpts: []ft.TestDirOpt{
				ft.FileContents("some-file.txt", "contents"),
			},
			wantError: true,
		},
		{
			name: "dir with file and force",
			testDirOpts: []ft.TestDirOpt{
				ft.FileContents("some-file.txt", "contents"),
			},
			args:      []string{"--force=true"},
			wantError: false,
		},
		{
			name: "dir with subdir",
			testDirOpts: []ft.TestDirOpt{
				ft.FileContents("parent/some-file.txt", "contents"),
			},
			wantError: true,
		},
	}

	// Usage information isn't useful in failed tests.
	Cmd.SilenceUsage = true

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resetFlags()

			testDir := ft.NewTestDir(t, tc.testDirOpts...)

			os.Args = append([]string{
				"init", // this first argument does nothing, but is required to exist.
				"--path", testDir.Root().OSPath(),
			}, tc.args...)

			// Run command
			err := Cmd.Execute()
			if tc.wantError {
				if err == nil {
					t.Error("got Initialize() = nil, want err")
				}
				return
			}

			if err != nil && !tc.wantError {
				t.Errorf("got Initialize() = %s, want nil", err)
				return
			}
		})
	}
}
