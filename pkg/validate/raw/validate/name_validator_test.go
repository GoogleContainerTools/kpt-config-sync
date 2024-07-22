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
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/status"
)

func TestName(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "object with name",
			obj:  k8sobjects.Deployment("/", core.Name("foo")),
		},
		{
			name:    "object with empty name fails",
			obj:     k8sobjects.Deployment("/", core.Name("")),
			wantErr: status.FakeError(nonhierarchical.MissingObjectNameErrorCode),
		},
		{
			name:    "object with invalid name fails",
			obj:     k8sobjects.Deployment("/", core.Name("FOO:BAR")),
			wantErr: status.FakeError(nonhierarchical.InvalidMetadataNameErrorCode),
		},
		{
			name: "object with valid name for RBAC",
			obj:  k8sobjects.Role(core.Name("FOO:BAR")),
		},
		{
			name:    "object with invalid name for RBAC fails",
			obj:     k8sobjects.Role(core.Name("FOO/BAR")),
			wantErr: status.FakeError(nonhierarchical.InvalidMetadataNameErrorCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Name(tc.obj)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got Name() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
