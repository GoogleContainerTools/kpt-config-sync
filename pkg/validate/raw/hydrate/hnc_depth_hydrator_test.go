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

package hydrate

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

func TestBuilderVisitor(t *testing.T) {
	testCases := []struct {
		name string
		objs *fileobjects.Raw
		want *fileobjects.Raw
	}{
		{
			name: "label and annotate namespace",
			objs: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo/bar"),
					k8sobjects.Namespace("namespaces/qux"),
					k8sobjects.Role(),
				},
			},
			want: &fileobjects.Raw{
				Objects: []ast.FileObject{
					k8sobjects.Namespace("namespaces/foo/bar",
						core.Annotation(metadata.HNCManagedBy, metadata.ManagedByValue),
						core.Label("foo.tree.hnc.x-k8s.io/depth", "1"),
						core.Label("bar.tree.hnc.x-k8s.io/depth", "0")),
					k8sobjects.Namespace("namespaces/qux",
						core.Annotation(metadata.HNCManagedBy, metadata.ManagedByValue),
						core.Label("qux.tree.hnc.x-k8s.io/depth", "0")),
					k8sobjects.Role(),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := HNCDepth(tc.objs); err != nil {
				t.Errorf("Got HNCDepth() error %v, want nil", err)
			}
			if diff := cmp.Diff(tc.want, tc.objs, ast.CompareFileObject); diff != "" {
				t.Error(diff)
			}
		})
	}
}
