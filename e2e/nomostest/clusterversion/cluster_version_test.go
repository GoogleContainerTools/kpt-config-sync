// Copyright 2024 Google LLC
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

package clusterversion

import (
	"testing"

	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/cli-utils/pkg/testutil"
)

func TestParseClusterVersion(t *testing.T) {
	testcases := []struct {
		name                   string
		input                  string
		expectedClusterVersion ClusterVersion
		expectedError          error
		expectedString         string
	}{
		{
			name:  "GKE patch",
			input: "v1.27.11-gke.1062001",
			expectedClusterVersion: ClusterVersion{
				Major:  1,
				Minor:  27,
				Patch:  11,
				Suffix: "-gke.1062001",
			},
			expectedString: "v1.27.11-gke.1062001",
		},
		{
			name:  "semver",
			input: "v1.27.11",
			expectedClusterVersion: ClusterVersion{
				Major: 1,
				Minor: 27,
				Patch: 11,
			},
			expectedString: "v1.27.11",
		},
		{
			name:  "semver without prefix",
			input: "1.27.11",
			expectedClusterVersion: ClusterVersion{
				Major: 1,
				Minor: 27,
				Patch: 11,
			},
			expectedString: "v1.27.11",
		},
		{
			name:  "major minor",
			input: "v1.27",
			expectedClusterVersion: ClusterVersion{
				Major: 1,
				Minor: 27,
			},
			expectedString: "v1.27.0",
		},
		{
			name:  "major",
			input: "v1",
			expectedClusterVersion: ClusterVersion{
				Major: 1,
			},
			expectedString: "v1.0.0",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			cv, err := ParseClusterVersion(tc.input)
			testutil.AssertEqual(t, tc.expectedClusterVersion, cv)
			testerrors.AssertEqual(t, tc.expectedError, err)
			testutil.AssertEqual(t, tc.expectedString, cv.String())
		})
	}
}
