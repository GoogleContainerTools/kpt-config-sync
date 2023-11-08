// Copyright 2023 Google LLC
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

package status

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetSourceHash(t *testing.T) {
	tests := map[string]struct {
		annotations    map[string]string
		expectedCommit string
	}{
		"should return empty when no annotations": {
			annotations:    nil,
			expectedCommit: "",
		},
		"should return empty when empty annotations": {
			annotations:    map[string]string{},
			expectedCommit: "",
		},
		"should return empty when annotation key doesn't exist": {
			annotations:    map[string]string{"foo": "bar"},
			expectedCommit: "",
		},
		"should return commit when annotation key exists": {
			annotations: map[string]string{
				"foo":                   "bar",
				SourceHashAnnotationKey: "1234567890",
			},
			expectedCommit: "1234567",
		},
	}
	for name, tc := range tests {
		t.Run(fmt.Sprintf("GetSourceHash %s", name), func(t *testing.T) {
			commit := GetSourceHash(tc.annotations)
			assert.Equal(t, tc.expectedCommit, commit)
		})
	}
}
