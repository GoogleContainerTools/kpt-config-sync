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

package git

import (
	"testing"
)

func TestCommitHash(t *testing.T) {
	for _, tc := range []struct {
		name    string
		dirPath string
		want    string
		wantErr bool
	}{
		{
			"valid commit hash",
			"/repo/3f8c6da2622fec5896c1e230bda3c53c17f61e8a",
			"3f8c6da2622fec5896c1e230bda3c53c17f61e8a",
			false,
		},
		{
			"invalid length",
			"/repo/abcdef123",
			"",
			true,
		},
		{
			"invalid characters",
			"/repo/zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz",
			"",
			true,
		},
		{
			"more characters after commit hash",
			"/repo/3f8c6da2622fec5896c1e230bda3c53c17f61e8a1111",
			"",
			true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CommitHash(tc.dirPath)
			if tc.wantErr {
				if err == nil {
					t.Errorf("CommitHash(%q) got nil error, want error", tc.dirPath)
				}
			} else if err != nil {
				t.Errorf("CommitHash(%q) got error %v, want nil error", tc.dirPath, err)
			}
			if got != tc.want {
				t.Errorf("CommitHash(%q) got %q, want %q", tc.dirPath, got, tc.want)
			}
		})
	}
}
