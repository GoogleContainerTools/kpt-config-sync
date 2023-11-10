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

package typeresolver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestResolve(t *testing.T) {
	r := fakeResolver()
	tests := map[string]struct {
		input         schema.GroupKind
		expectFound   bool
		expectVersion string
	}{
		"non existing type return false": {
			input:       schema.GroupKind{Group: "not.exist", Kind: "UnFound"},
			expectFound: false,
		},
		"should have ConfigMap": {
			input:         schema.GroupKind{Group: "", Kind: "ConfigMap"},
			expectFound:   true,
			expectVersion: "v1",
		},
		"should have Deployment": {
			input:         schema.GroupKind{Group: "apps", Kind: "Deployment"},
			expectFound:   true,
			expectVersion: "v1",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gvk, found := r.Resolve(tc.input)
			assert.Equal(t, tc.expectFound, found)
			assert.Equal(t, tc.expectVersion, gvk.Version)
		})
	}
}
