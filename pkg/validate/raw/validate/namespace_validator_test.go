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
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/status"
)

func TestNamespace(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "Role with unspecified namespace",
			obj:  k8sobjects.Role(core.Namespace("")),
		},
		{
			name: "Role with valid namespace",
			obj:  k8sobjects.Role(core.Namespace("hello")),
		},
		{
			name:    "Role with invalid namespace",
			obj:     k8sobjects.Role(core.Namespace("..invalid..")),
			wantErr: nonhierarchical.InvalidNamespaceError(k8sobjects.Role()),
		},
		{
			name: "RootSync with config-management-system namespace",
			obj:  k8sobjects.RootSyncV1Beta1("foo"),
		},
		{
			name: "Valid namespace",
			obj:  k8sobjects.Namespace("hello"),
		},
		{
			name:    "Illegal namespace " + configmanagement.ControllerNamespace,
			obj:     k8sobjects.Namespace(configmanagement.ControllerNamespace),
			wantErr: nonhierarchical.IllegalNamespace(k8sobjects.Namespace(configmanagement.ControllerNamespace)),
		},
		{
			name:    "Illegal namespace " + configmanagement.RGControllerNamespace,
			obj:     k8sobjects.Namespace(configmanagement.RGControllerNamespace),
			wantErr: nonhierarchical.IllegalNamespace(k8sobjects.Namespace(configmanagement.RGControllerNamespace)),
		},
		{
			name:    "Illegal namespace " + configmanagement.MonitoringNamespace,
			obj:     k8sobjects.Namespace(configmanagement.MonitoringNamespace),
			wantErr: nonhierarchical.IllegalNamespace(k8sobjects.Namespace(configmanagement.MonitoringNamespace)),
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
