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
	"os/exec"
	"strings"

	"github.com/pkg/errors"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
)

// FindFiles lists what are likely the files tracked by git in cases where
// we may not be dealing with a git repository. ONLY FOR USE IN THE CLI.
//
// Tries git first, and falls back to using `find` if git does not work.
//
// Guaranteed to return the same files as ListFiles in git repo with no
// uncommitted changes (see tests for findFiles)
func FindFiles(dir cmpath.Absolute) ([]cmpath.Absolute, error) {
	out, err := exec.Command("find", dir.OSPath(), "-type", "f", "-not", "-path", "*/\\.git/*").CombinedOutput()
	if err != nil {
		return nil, errors.Wrap(err, string(out))
	}
	files := strings.Split(string(out), "\n")
	var result []cmpath.Absolute
	// The output from git find, when split on newline, will include an empty string at the end which we don't want.
	for _, f := range files[:len(files)-1] {
		p, err := cmpath.AbsoluteOS(f)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}
