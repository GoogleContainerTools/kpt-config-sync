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
	"kpt.dev/configsync/pkg/syncer/syncertest"
)

func TestUnmanagedNamespaces(t *testing.T) {
	testCases := []struct {
		name     string
		objs     []ast.FileObject
		wantErrs status.MultiError
	}{
		{
			name: "Cluster-scoped objects pass",
			objs: []ast.FileObject{
				k8sobjects.ClusterRole(),
				k8sobjects.ClusterRole(syncertest.ManagementDisabled),
			},
		},
		{
			name: "Unmanaged namespace-scoped objects in managed namespace pass",
			objs: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo"),
				k8sobjects.Role(core.Namespace("foo")),
				k8sobjects.Role(core.Namespace("foo"), syncertest.ManagementDisabled),
			},
		},
		{
			name: "Unmanaged namespace-scoped object in unmanaged namespace passes",
			objs: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo", syncertest.ManagementDisabled),
				k8sobjects.Role(core.Namespace("foo"), syncertest.ManagementDisabled),
			},
		},
		{
			name: "Managed namespace-scoped object in unmanaged namespace fails",
			objs: []ast.FileObject{
				k8sobjects.Namespace("namespaces/foo", syncertest.ManagementDisabled),
				k8sobjects.Role(core.Namespace("foo")),
			},
			wantErrs: status.FakeMultiError(nonhierarchical.ManagedResourceInUnmanagedNamespaceErrorCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := UnmanagedNamespaces(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got UnmanagedNamespaces() error %v, want %v", errs, tc.wantErrs)
			}
		})
	}
}
