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
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

func TestNamespaceSelectors(t *testing.T) {
	testCases := []struct {
		name     string
		objs     *fileobjects.Scoped
		wantErrs status.MultiError
	}{
		{
			name: "No objects",
			objs: &fileobjects.Scoped{},
		},
		{
			name: "One NamespaceSelector",
			objs: &fileobjects.Scoped{
				Cluster: []ast.FileObject{
					k8sobjects.NamespaceSelector(core.Name("first")),
				},
			},
		},
		{
			name: "Two NamespaceSelectors",
			objs: &fileobjects.Scoped{
				Cluster: []ast.FileObject{
					k8sobjects.NamespaceSelector(core.Name("first")),
					k8sobjects.NamespaceSelector(core.Name("second")),
				},
			},
		},
		{
			name: "Duplicate NamespaceSelectors",
			objs: &fileobjects.Scoped{
				Cluster: []ast.FileObject{
					k8sobjects.NamespaceSelector(core.Name("first")),
					k8sobjects.NamespaceSelector(core.Name("first")),
				},
			},
			wantErrs: nonhierarchical.SelectorMetadataNameCollisionError(kinds.NamespaceSelector().Kind, "first", k8sobjects.NamespaceSelector()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := NamespaceSelectors(tc.objs)
			if !errors.Is(errs, tc.wantErrs) {
				t.Errorf("got NamespaceSelectors() error %v, want %v", errs, tc.wantErrs)
			}
		})
	}
}
