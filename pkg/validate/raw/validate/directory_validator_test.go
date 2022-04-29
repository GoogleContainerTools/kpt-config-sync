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
	"kpt.dev/configsync/pkg/importer/analyzer/validation/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestDirectory(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "Role with unspecified namespace",
			obj:  fake.RoleAtPath("namespaces/hello/role.yaml", core.Namespace("")),
		},
		{
			name: "Role under valid directory",
			obj:  fake.RoleAtPath("namespaces/hello/role.yaml", core.Namespace("hello")),
		},
		{
			name:    "Role under invalid directory",
			obj:     fake.RoleAtPath("namespaces/hello/role.yaml", core.Namespace("world")),
			wantErr: metadata.IllegalMetadataNamespaceDeclarationError(fake.Role(core.Namespace("world")), "hello"),
		},
		{
			name: "Namespace under valid directory",
			obj:  fake.Namespace("namespaces/hello"),
		},
		{
			name:    "Namespace under invalid directory",
			obj:     fake.Namespace("namespaces/hello", core.Name("world")),
			wantErr: metadata.InvalidNamespaceNameError(fake.Namespace("namespaces/hello", core.Name("world")), "hello"),
		},
		{
			name:    "Namespace under top-level namespaces directory",
			obj:     fake.Namespace("namespaces", core.Name("hello")),
			wantErr: metadata.IllegalTopLevelNamespaceError(fake.Namespace("namespaces", core.Name("hello"))),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Directory(tc.obj)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got Directory() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
