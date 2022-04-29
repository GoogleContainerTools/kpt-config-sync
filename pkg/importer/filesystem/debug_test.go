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

package filesystem_test

import (
	"testing"

	"kpt.dev/configsync/pkg/importer/filesystem"
	ft "kpt.dev/configsync/pkg/importer/filesystem/filesystemtest"
)

func TestWalkDirectory(t *testing.T) {
	// add .git/ and .git/test_dir to /tmp/nomos-test-XXXX directory for testing.
	dir := ft.NewTestDir(t,
		ft.FileContents(".git/test_dir.yaml", "test content"),
	).Root()

	d, err := filesystem.WalkDirectory(dir.OSPath())
	if err != nil {
		t.Fatalf("got WalkDirectory() = %v, want nil", err)
	}
	// Check whether /tmp/nomos-test-XXXX/test_dir.yaml skipped.
	if len(d) > 2 {
		t.Errorf("got WalkDirectory(.git sub-directories traversed) = %v, want 2", d)
	}
}
