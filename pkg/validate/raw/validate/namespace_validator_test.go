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

	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestNamespace(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "Role with unspecified namespace",
			obj:  fake.Role(core.Namespace("")),
		},
		{
			name: "Role with valid namespace",
			obj:  fake.Role(core.Namespace("hello")),
		},
		{
			name:    "Role with invalid namespace",
			obj:     fake.Role(core.Namespace("..invalid..")),
			wantErr: nonhierarchical.InvalidNamespaceError(fake.Role()),
		},
		{
			name:    "Role with illegal namespace",
			obj:     fake.Role(core.Namespace(configmanagement.ControllerNamespace)),
			wantErr: nonhierarchical.ObjectInIllegalNamespace(fake.Role()),
		},
		{
			name: "RootSync with config-management-system namespace",
			obj:  fake.RootSyncV1Beta1("foo"),
		},
		{
			name:    "RepoSync with config-management-system namespace",
			obj:     fake.RepoSyncV1Beta1(configmanagement.ControllerNamespace, "foo"),
			wantErr: nonhierarchical.ObjectInIllegalNamespace(fake.RepoSyncV1Beta1(configmanagement.ControllerNamespace, "foo")),
		},
		{
			name: "Valid namespace",
			obj:  fake.Namespace("hello"),
		},
		{
			name:    "Illegal namespace",
			obj:     fake.Namespace(configmanagement.ControllerNamespace),
			wantErr: nonhierarchical.IllegalNamespace(fake.Namespace(configmanagement.ControllerNamespace)),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := Namespace(tc.obj)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got Namespace() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
