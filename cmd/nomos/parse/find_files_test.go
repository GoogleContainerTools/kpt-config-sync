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
	"io/ioutil"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/importer/git"
)

func TestListPolicyFiles(t *testing.T) {
	testCases := []struct {
		name  string
		files []string
		want  []string
	}{
		{
			name: "empty returns empty",
		},
		{
			name:  "read .yml, .yaml, and .json",
			files: []string{"ns.yaml", "role.yml", "rb.json"},
			want:  []string{"ns.yaml", "role.yml", "rb.json"},
		},
		{
			name:  "read subdirectory",
			files: []string{"namespaces/foo/", "namespaces/foo/ns.yaml"},
			want:  []string{"namespaces/foo/ns.yaml"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {

			dirP, err := ioutil.TempDir(os.TempDir(), "nomos-git-test")
			if err != nil {
				t.Fatal(err)
			}
			dir, err := cmpath.AbsoluteOS(dirP)
			if err != nil {
				t.Fatal(err)
			}

			// Initialize a git repository.
			out, err := exec.Command("git", "-C", dir.OSPath(), "init").CombinedOutput()
			if err != nil {
				t.Fatal(errors.Wrap(err, string(out)))
			}

			// Add all of the specified files to the repository.
			for _, f := range tc.files {
				p := cmpath.RelativeSlash(f)
				if strings.HasSuffix(f, "/") {
					err = os.MkdirAll(dir.Join(p).OSPath(), os.ModePerm)
					if err != nil {
						t.Fatal(err)
					}
				} else {
					file, err := os.Create(dir.Join(p).OSPath())
					if err != nil {
						t.Fatal(err)
					}
					err = file.Close()
					if err != nil {
						t.Fatal(err)
					}
					out, err = exec.Command("git", "-C", dir.OSPath(), "add", f).CombinedOutput()
					if err != nil {
						t.Fatal(errors.Wrap(err, string(out)))
					}
				}
			}

			// Commit. Note that the identification fields are required as this
			// may be running in a container without a git config set up.
			if len(tc.files) > 0 {
				out, err = exec.Command("git",
					"-c", "user.name='Test'",
					"-c", "user.email='team@example.comcmpath'",
					"-C", dir.OSPath(), "commit", "-m", "add files").CombinedOutput()
				if err != nil {
					t.Fatal(errors.Wrap(err, string(out)))
				}
			}

			abs, err := dir.EvalSymlinks()
			if err != nil {
				t.Fatal(err)
			}

			resultGit, err := git.ListFiles(abs)
			if err != nil {
				t.Fatal(err)
			}
			sort.Slice(resultGit, func(i, j int) bool {
				return resultGit[i].OSPath() < resultGit[j].OSPath()
			})
			resultFind, err := FindFiles(abs)
			if err != nil {
				t.Fatal(err)
			}
			sort.Slice(resultFind, func(i, j int) bool {
				return resultFind[i].OSPath() < resultFind[j].OSPath()
			})
			if diff := cmp.Diff(resultGit, resultFind); diff != "" {
				t.Errorf("diff between ListFiles and FindFiles:\n%s", diff)
			}

			sort.Strings(tc.want)
			var want []string
			for _, w := range tc.want {
				// Since ListFiles returns absolute paths, we have to convert
				// these to the expected absolute paths that include the randomly-generated
				// temp diretory.
				want = append(want, dir.Join(cmpath.RelativeSlash(w)).OSPath())
			}

			var got []string
			for _, r := range resultGit {
				got = append(got, r.SlashPath())
			}
			sort.Strings(got)

			if diff := cmp.Diff(want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}
