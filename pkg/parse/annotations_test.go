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

package parse

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestAddAnnotationsAndLabels(t *testing.T) {
	testcases := []struct {
		name       string
		actual     []ast.FileObject
		expected   []ast.FileObject
		gc         sourceContext
		commitHash string
	}{
		{
			name:     "empty list",
			actual:   []ast.FileObject{},
			expected: []ast.FileObject{},
		},
		{
			name: "nil annotation without env",
			gc: sourceContext{
				Repo:   "git@github.com/foo",
				Branch: "main",
				Rev:    "HEAD",
			},
			commitHash: "1234567",
			actual:     []ast.FileObject{fake.Role(core.Namespace("foo"))},
			expected: []ast.FileObject{fake.Role(
				core.Namespace("foo"),
				core.Label(metadata.ManagedByKey, metadata.ManagedByValue),
				core.Annotation(metadata.ResourceManagementKey, "enabled"),
				core.Annotation(metadata.ResourceManagerKey, "some-namespace_rs"),
				core.Annotation(metadata.SyncTokenAnnotationKey, "1234567"),
				core.Annotation(metadata.GitContextKey, `{"repo":"git@github.com/foo","branch":"main","rev":"HEAD"}`),
				core.Annotation(metadata.OwningInventoryKey, applier.InventoryID("rs", "some-namespace")),
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_foo_default-name"),
			)},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if err := addAnnotationsAndLabels(tc.actual, "some-namespace", "rs", tc.gc, tc.commitHash); err != nil {
				t.Fatalf("Failed to add annotations and labels: %v", err)
			}
			if diff := cmp.Diff(tc.expected, tc.actual, ast.CompareFileObject); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}
