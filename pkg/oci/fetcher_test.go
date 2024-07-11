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

package oci

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasDigest(t *testing.T) {
	testCases := map[string]struct {
		input  string
		output bool
	}{
		"tag and digest": {
			input:  "gcr.io/foo/bar:v1.0.0@sha256:c8c72263b8ca7c22578d1a56373c5dd7d9ac33ef12faf7bea4945c89ef607e75",
			output: true,
		},
		"tag": {
			input:  "gcr.io/foo/bar:v1.0.0",
			output: false,
		},
		"digest": {
			input:  "gcr.io/foo/bar@sha256:c8c72263b8ca7c22578d1a56373c5dd7d9ac33ef12faf7bea4945c89ef607e75",
			output: true,
		},
		"invalid": {
			input:  "some/other/thing",
			output: false,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.output, HasDigest(tc.input))
		})
	}
}
