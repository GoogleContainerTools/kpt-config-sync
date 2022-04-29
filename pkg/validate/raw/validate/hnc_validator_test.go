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
	"kpt.dev/configsync/pkg/importer/analyzer/hnc"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

const (
	illegalSuffixedLabel  = "unsupported" + metadata.DepthSuffix
	illegalSuffixedLabel2 = "unsupported2" + metadata.DepthSuffix
)

func TestHNCLabels(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "no labels",
			obj:  fake.RoleAtPath("namespaces/hello/role.yaml"),
		},
		{
			name: "one legal label",
			obj: fake.RoleAtPath("namespaces/hello/role.yaml",
				core.Label(legalLabel, "")),
		},
		{
			name: "one illegal label",
			obj: fake.RoleAtPath("namespaces/hello/role.yaml",
				core.Label(illegalSuffixedLabel, "")),
			wantErr: hnc.IllegalDepthLabelError(fake.Role(), []string{illegalSuffixedLabel}),
		},
		{
			name: "two illegal labels",
			obj: fake.RoleAtPath("namespaces/hello/role.yaml",
				core.Label(illegalSuffixedLabel, ""),
				core.Label(illegalSuffixedLabel2, "")),
			wantErr: hnc.IllegalDepthLabelError(fake.Role(), []string{illegalSuffixedLabel, illegalSuffixedLabel2}),
		},
		{
			name: "one legal and one illegal label",
			obj: fake.RoleAtPath("namespaces/hello/role.yaml",
				core.Label(legalLabel, ""),
				core.Label(illegalSuffixedLabel, "")),
			wantErr: hnc.IllegalDepthLabelError(fake.Role(), []string{illegalSuffixedLabel}),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := HNCLabels(tc.obj)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got HNCLabels() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
