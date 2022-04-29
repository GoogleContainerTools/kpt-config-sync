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

package validate

import (
	"errors"
	"testing"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util/repo"
	"kpt.dev/configsync/pkg/validate/objects"
)

const notAllowedRepoVersion = "0.0.0"

func TestRepo(t *testing.T) {
	testCases := []struct {
		name    string
		objs    *objects.Raw
		wantErr status.Error
	}{
		{
			name: "Repo with current version",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Repo(fake.RepoVersion(repo.CurrentVersion)),
				},
			},
		},
		{
			name: "Repo with supported old version",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Repo(fake.RepoVersion(system.OldAllowedRepoVersion)),
				},
			},
		},
		{
			name: "Repo with unsupported old version",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Repo(fake.RepoVersion(notAllowedRepoVersion)),
				},
			},
			wantErr: system.UnsupportedRepoSpecVersion(fake.Repo(fake.RepoVersion(notAllowedRepoVersion)), notAllowedRepoVersion),
		},
		{
			name: "Missing Repo",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Role(),
					fake.RoleBinding(),
				},
			},
			wantErr: system.MissingRepoError(),
		},
		{
			name: "Multiple Repos",
			objs: &objects.Raw{
				Objects: []ast.FileObject{
					fake.Repo(core.Name("first")),
					fake.Repo(core.Name("second")),
				},
			},
			wantErr: status.MultipleSingletonsError(fake.Repo(), fake.Repo()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Repo(tc.objs)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Got Repo() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
