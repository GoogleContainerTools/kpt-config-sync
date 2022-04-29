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

package filesystemtest

import (
	"io/ioutil"
	"os"
	"path"
	"testing"

	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/reader"
)

// FileContentMap specifies files that should be created as part of a parser
// test.
//
// Keys are slash-delimited paths.
type FileContentMap map[string]string

// TestDirOpt performs setup on the test directory in question.
type TestDirOpt func(t *testing.T, testDir cmpath.Absolute)

// TestDir is a temporary directory for use in testing.
type TestDir struct {
	tmpDir    cmpath.Absolute
	policyDir cmpath.Relative
}

// NewTestDir constructs a new temporary test directory.
//
// The test directory is automatically cleaned at the end of the test.
func NewTestDir(t *testing.T, opts ...TestDirOpt) *TestDir {
	t.Helper()
	tmp, err := ioutil.TempDir("", "nomos-test-")
	if err != nil {
		t.Fatalf("Failed to create test directory %v", err)
	}
	abs, err := cmpath.AbsoluteOS(tmp)
	if err != nil {
		t.Fatalf("Failed to create test directory %v", err)
	}
	rel := cmpath.RelativeOS(tmp)
	result := &TestDir{tmpDir: abs, policyDir: rel}
	t.Cleanup(func() {
		result.remove(t)
	})

	for _, opt := range opts {
		opt(t, abs)
	}
	return result
}

// Root returns the absolute path to the root directory of the TestDir.
func (d TestDir) Root() cmpath.Absolute {
	return d.tmpDir
}

// FilePaths returns a collection of absolute file paths along with the absolute
// and relative paths of the root directory of the TestDir.
//
// filePaths is a list of slash-delimited paths relative to the test directory root.
func (d TestDir) FilePaths(filePaths ...string) reader.FilePaths {
	var files []cmpath.Absolute
	for _, f := range filePaths {
		files = append(files, d.tmpDir.Join(cmpath.RelativeSlash(f)))
	}

	return reader.FilePaths{
		RootDir:   d.tmpDir,
		PolicyDir: d.policyDir,
		Files:     files,
	}
}

// Remove deletes the test directory.
func (d TestDir) remove(t *testing.T) {
	t.Helper()
	if err := os.RemoveAll(d.tmpDir.OSPath()); err != nil {
		// It is an error to be unable to remove the test directory at the end of
		// the test.
		t.Errorf("unable to clean up test directory %q: %v", d.tmpDir.OSPath(), err)
	}
}

// FileContents writes a file called subPath with contents inside the test
// directory.
//
// file is a slash-delimited path relative to the test directory root.
func FileContents(file string, contents string) TestDirOpt {
	return func(t *testing.T, testDir cmpath.Absolute) {
		Dir(path.Dir(file))(t, testDir)
		p := testDir.Join(cmpath.RelativeSlash(file))
		if err := ioutil.WriteFile(p.OSPath(), []byte(contents), os.ModePerm); err != nil {
			t.Fatalf("writing contents to file %q: %v", p.OSPath(), err)
		}
	}
}

// Chmod changes the permissions of the passed path to perm.
//
// file is a slash-delimited path relative to the test directory root.
func Chmod(file string, perm os.FileMode) TestDirOpt {
	return func(t *testing.T, testDir cmpath.Absolute) {
		t.Helper()
		p := testDir.Join(cmpath.RelativeSlash(file))
		t.Cleanup(func() {
			err := os.Chmod(p.OSPath(), os.ModePerm)
			if err != nil {
				t.Errorf("resetting permissions on %q: %v", p.OSPath(), err)
			}
		})

		err := os.Chmod(p.OSPath(), perm)
		if err != nil {
			t.Fatalf("changing permisisons on %q: %v", p.OSPath(), err)
		}
	}
}

// Dir creates a directory inside the test directory if it does not exist.
//
// dir is a slash-delimited relative path.
func Dir(dir string) TestDirOpt {
	return func(t *testing.T, testDir cmpath.Absolute) {
		t.Helper()
		p := testDir.Join(cmpath.RelativeSlash(dir))
		if err := os.MkdirAll(p.OSPath(), os.ModePerm); err != nil {
			t.Fatalf("creating directory %q: %v", p.OSPath(), err)
		}
	}
}

// DirContents is a convenience method for writing many files inside the test
// directory.
func DirContents(contentMap FileContentMap) TestDirOpt {
	return func(t *testing.T, testDir cmpath.Absolute) {
		t.Helper()
		for file, contents := range contentMap {
			FileContents(file, contents)(t, testDir)
		}
	}
}
