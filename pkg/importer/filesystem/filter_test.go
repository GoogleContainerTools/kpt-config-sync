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

package filesystem

import (
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
)

func toCmpath(t *testing.T, files []string) []cmpath.Absolute {
	var result []cmpath.Absolute
	for _, f := range files {
		p, err := cmpath.AbsoluteSlash(f)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, p)
	}
	return result
}

func fromCmpath(paths []cmpath.Absolute) []string {
	var result []string
	for _, p := range paths {
		result = append(result, p.SlashPath())
	}
	return result
}

func TestFilterHierarchyFiles(t *testing.T) {
	testCases := []struct {
		name  string
		root  string
		files []string
		want  []string
	}{
		{
			name: "empty works",
			root: "/",
		},
		{
			name:  "root doesn't panic",
			root:  "/",
			files: []string{"/"},
		},
		{
			name:  "keep system/",
			root:  "/",
			files: []string{"/system/repo.yaml"},
			want:  []string{"/system/repo.yaml"},
		},
		{
			name:  "keep cluster/",
			root:  "/",
			files: []string{"/cluster/cr.yaml"},
			want:  []string{"/cluster/cr.yaml"},
		},
		{
			name:  "keep clusterregistry/",
			root:  "/",
			files: []string{"/clusterregistry/cluster.yaml"},
			want:  []string{"/clusterregistry/cluster.yaml"},
		},
		{
			name:  "keep namespaces/",
			root:  "/",
			files: []string{"/namespaces/ns.yaml"},
			want:  []string{"/namespaces/ns.yaml"},
		},
		{
			name:  "ignore top-level",
			root:  "/",
			files: []string{"/namespaces.yaml"},
		},
		{
			name:  "ignore other subdirectory",
			root:  "/",
			files: []string{"/other/repo.yaml"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			root, err := cmpath.AbsoluteSlash(tc.root)
			if err != nil {
				t.Fatal(err)
			}

			got := fromCmpath(FilterHierarchyFiles(root, toCmpath(t, tc.files)))

			sort.Strings(tc.want)
			sort.Strings(got)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Error(diff)
			}
		})
	}
}
