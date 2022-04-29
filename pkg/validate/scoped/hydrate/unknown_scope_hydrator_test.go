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
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/validate/objects"
)

func TestUnknownScope(t *testing.T) {
	testCases := []struct {
		name string
		objs *objects.Scoped
		want *objects.Scoped
	}{
		{
			name: "No objects",
			objs: &objects.Scoped{},
			want: &objects.Scoped{},
		},
		{
			name: "no unknown scope objects",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Namespace("prod")),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Namespace("prod")),
				},
			},
		},
		{
			name: "has unknown scope objects",
			objs: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
					fake.Namespace("namespaces/dev2", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(core.Namespace("prod")),
				},
				Unknown: []ast.FileObject{
					fake.FileObject(fake.RootSyncObjectV1Beta1(configsync.RootSyncName), "rootsync.yaml"),
				},
			},
			want: &objects.Scoped{
				Cluster: []ast.FileObject{
					fake.Namespace("namespaces/prod", core.Label("environment", "prod")),
					fake.Namespace("namespaces/dev1", core.Label("environment", "dev")),
					fake.Namespace("namespaces/dev2", core.Label("environment", "dev")),
				},
				Namespace: []ast.FileObject{
					fake.Role(
						core.Namespace("prod"),
					),
				},
				Unknown: []ast.FileObject{
					fake.FileObject(fake.RootSyncObjectV1Beta1(configsync.RootSyncName,
						core.Annotation(metadata.UnknownScopeAnnotationKey, metadata.UnknownScopeAnnotationValue),
					), "rootsync.yaml"),
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := UnknownScope(tc.objs)
			if errs != nil {
				t.Errorf("Got UnknownScope() error %v, want nil", errs)
			}
			if diff := cmp.Diff(tc.want, tc.objs, ast.CompareFileObject); diff != "" {
				t.Error(diff)
			}
		})
	}
}
