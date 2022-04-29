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

package hydrate

import (
	"fmt"
	"testing"
)

func TestValidateTool(t *testing.T) {
	testCases := []struct {
		name        string
		version     string
		expectedErr string
	}{
		{
			name:        "tool version is too old",
			version:     "{kustomize/v3.6.5  2021-05-20T20:52:40Z  }",
			expectedErr: fmt.Sprintf(`The current kustomize version is "3.6.5". The recommended version is %s. Please upgrade to the %s+ for compatibility.`, KustomizeVersion, KustomizeVersion),
		},
		{
			name:    "tool version is the same as required",
			version: fmt.Sprintf("{kustomize/%s  2021-08-24T20:52:40Z  }", KustomizeVersion),
		},
		{
			name:    "tool version is newer than required",
			version: "{kustomize/v8.4.4  2025-05-20T20:52:40Z  }",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateTool(Kustomize, tc.version, KustomizeVersion)
			if err != nil && tc.expectedErr == "" {
				t.Errorf("%s: expected no error, but got error: %v", tc.name, err)
			} else if err == nil && tc.expectedErr != "" {
				t.Errorf("%s: got no error, but expected error: %v", tc.name, tc.expectedErr)
			} else if err != nil && tc.expectedErr != "" && err.Error() != tc.expectedErr {
				t.Errorf("%s: got error: %v, but expected: %v", tc.name, err, tc.expectedErr)
			}
		})
	}
}

func TestNeedsKustomize(t *testing.T) {
	testCases := []struct {
		name   string
		dir    string
		result bool
	}{
		{
			name:   "A wet repo doesn't need kustomization",
			dir:    "../../e2e/testdata/hydration/wet-repo",
			result: false,
		},
		{
			name:   "A repo has a kustomization.yaml file",
			dir:    "../../e2e/testdata/hydration/helm-components",
			result: true,
		},
		{
			name:   "A repo has a kustomization.yml file",
			dir:    "../../e2e/testdata/hydration/kustomize-components",
			result: true,
		},
		{
			name:   "A repo has a kustomization.yml file in the nested directory",
			dir:    "../../e2e/testdata/hydration",
			result: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			needs, err := needsKustomize(tc.dir)
			if err != nil {
				t.Errorf("%s: expected no error, but got error: %v", tc.name, err)
			} else if needs != tc.result {
				t.Errorf("%s: expected %t, but got %t", tc.name, tc.result, needs)
			}
		})
	}
}
