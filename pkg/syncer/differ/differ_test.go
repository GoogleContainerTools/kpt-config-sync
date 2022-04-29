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

package differ

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
)

func buildUnstructured(opts ...func(*unstructured.Unstructured)) *unstructured.Unstructured {
	result := &unstructured.Unstructured{}
	for _, opt := range opts {
		opt(result)
	}
	return result
}

func name(s string) func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		u.SetName(s)
	}
}

func managed(s string) func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		core.SetAnnotation(u, metadata.ResourceManagementKey, s)
	}
}

func managedByConfigSync() func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		core.SetAnnotation(u, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
		core.SetAnnotation(u, metadata.ResourceIDKey, core.GKNN(u))
	}
}

func tokenAnnotation(s string) func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		core.SetAnnotation(u, metadata.SyncTokenAnnotationKey, s)
	}
}

func owned() func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		owners := u.GetOwnerReferences()
		owners = append(owners, metav1.OwnerReference{})
		u.SetOwnerReferences(owners)
	}
}

func preventDeletionUnstructured() func(*unstructured.Unstructured) {
	return func(u *unstructured.Unstructured) {
		preventDeletion(u)
	}
}

func TestDiffType(t *testing.T) {
	testCases := []struct {
		name       string
		declared   *unstructured.Unstructured
		actual     *unstructured.Unstructured
		expectType Type
	}{
		{
			name:       "in repo, create",
			declared:   buildUnstructured(),
			expectType: Create,
		},
		{
			name:       "in repo only and unmanaged, noop",
			declared:   buildUnstructured(managed(metadata.ResourceManagementDisabled)),
			expectType: NoOp,
		},
		{
			name:       "in repo only, management invalid error",
			declared:   buildUnstructured(managed("invalid")),
			expectType: Error,
		},
		{
			name:       "in repo only, management empty string error",
			declared:   buildUnstructured(managed("")),
			expectType: Error,
		},
		{
			name:       "in both, update",
			declared:   buildUnstructured(),
			actual:     buildUnstructured(),
			expectType: Update,
		},
		{
			name:       "in both and owned, update",
			declared:   buildUnstructured(),
			actual:     buildUnstructured(owned()),
			expectType: Update,
		},
		{
			name:       "in both, update even though cluster has invalid annotation",
			declared:   buildUnstructured(),
			actual:     buildUnstructured(managed("invalid")),
			expectType: Update,
		},
		{
			name:       "in both (the actual resource has no nomos metadata), update",
			declared:   buildUnstructured(),
			actual:     buildUnstructured(),
			expectType: Update,
		},
		{
			name:       "in both, management disabled unmanage",
			declared:   buildUnstructured(managed(metadata.ResourceManagementDisabled)),
			actual:     buildUnstructured(managed(metadata.ResourceManagementEnabled)),
			expectType: Unmanage,
		},
		{
			name:       "in both, management disabled noop",
			declared:   buildUnstructured(managed(metadata.ResourceManagementDisabled)),
			actual:     buildUnstructured(),
			expectType: NoOp,
		},
		{
			name:       "in cluster only, does not have the `configsync.gke.io./resource-id` annotation",
			actual:     buildUnstructured(managed(metadata.ResourceManagementEnabled)),
			expectType: NoOp,
		},
		{
			name:       "in cluster only, delete a resource managed by Config Sync",
			actual:     buildUnstructured(managedByConfigSync()),
			expectType: Delete,
		},
		{
			name:       "in cluster only, unset noop",
			actual:     buildUnstructured(),
			expectType: NoOp,
		},
		{
			name:       "in cluster only, has the `configmanagement.gke.io/token` annotation, but is not managed by Config Sync",
			actual:     buildUnstructured(tokenAnnotation("token1")),
			expectType: NoOp,
		},
		{
			name:       "in cluster only, the `configmanagement.gke.io/managed` annotation is empty",
			actual:     buildUnstructured(managed("")),
			expectType: NoOp,
		},
		{
			name:       "in cluster only, has an invalid `configmanagement.gke.io/managed` annotation",
			actual:     buildUnstructured(managed("invalid")),
			expectType: NoOp,
		},
		{
			name:       "in cluster only, has an invalid `configmanagement.gke.io/managed`  and other nomos metatdatas",
			actual:     buildUnstructured(managed("invalid"), tokenAnnotation("token1")),
			expectType: NoOp,
		},
		{
			name:       "in cluster only and owned, do nothing",
			actual:     buildUnstructured(managedByConfigSync(), owned()),
			expectType: NoOp,
		},
		{
			name: "in cluster only and owned and prevent deletion, unmanage",
			actual: buildUnstructured(managedByConfigSync(), owned(),
				preventDeletionUnstructured()),
			expectType: NoOp,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			diff := Diff{
				Declared: tc.declared,
				Actual:   tc.actual,
			}

			if d := cmp.Diff(tc.expectType, diff.Type()); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestMultipleDifftypes(t *testing.T) {
	testcases := []struct {
		name                string
		declared            []*unstructured.Unstructured
		actuals             []*unstructured.Unstructured
		allDeclaredVersions map[string]bool
		expect              map[string]*Diff
		expectTypes         map[string]Type
		expectPanic         bool
	}{
		{
			name:        "empty returns empty",
			expect:      map[string]*Diff{},
			expectTypes: map[string]Type{},
		},
		{
			name: "not declared and in actual and managed by Config Sync, but in different version returns no diff",
			actuals: []*unstructured.Unstructured{
				buildUnstructured(name("foo"), managedByConfigSync()),
			},
			allDeclaredVersions: map[string]bool{"foo": true},
			expect:              map[string]*Diff{},
			expectTypes:         map[string]Type{},
		},
		{
			name: "multiple diff types works",
			declared: []*unstructured.Unstructured{
				buildUnstructured(name("foo")),
				buildUnstructured(name("bar")),
				buildUnstructured(name("qux"), managed(metadata.ResourceManagementDisabled)),
			},
			actuals: []*unstructured.Unstructured{
				buildUnstructured(name("bar"), managedByConfigSync()),
				buildUnstructured(name("qux")),
				buildUnstructured(name("mun"), managedByConfigSync()),
				buildUnstructured(name("mun-not-managed-by-ConfigSync")),
			},
			allDeclaredVersions: map[string]bool{
				"foo": true, "bar": true, "qux": true,
			},
			expect: map[string]*Diff{
				"foo": {
					Name:     "foo",
					Declared: buildUnstructured(name("foo")),
				},
				"bar": {
					Name:     "bar",
					Declared: buildUnstructured(name("bar")),
					Actual:   buildUnstructured(name("bar"), managedByConfigSync()),
				},
				"qux": {
					Name:     "qux",
					Declared: buildUnstructured(name("qux"), managed(metadata.ResourceManagementDisabled)),
					Actual:   buildUnstructured(name("qux")),
				},
				"mun": {
					Name:   "mun",
					Actual: buildUnstructured(name("mun"), managedByConfigSync()),
				},
				"mun-not-managed-by-ConfigSync": {
					Name:   "mun-not-managed-by-ConfigSync",
					Actual: buildUnstructured(name("mun-not-managed-by-ConfigSync")),
				},
			},
			expectTypes: map[string]Type{
				"foo":                           Create,
				"bar":                           Update,
				"qux":                           NoOp,
				"mun":                           Delete,
				"mun-not-managed-by-ConfigSync": NoOp,
			},
		},
		{
			name: "duplicate declarations panics",
			declared: []*unstructured.Unstructured{
				buildUnstructured(name("foo")),
				buildUnstructured(name("foo")),
			},
			expectPanic: true,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if x := recover(); x != nil {
					if _, ok := x.(invalidInput); ok && tc.expectPanic {
						return
					}
					t.Fatal(x)
				}
			}()

			diffs := Diffs(tc.declared, tc.actuals, tc.allDeclaredVersions)

			if len(tc.declared) > 0 {
				fmt.Printf("%v\n", tc.declared[0].Object)
				fmt.Println("name: ", tc.declared[0].GetName())
			}

			diffsMap := make(map[string]*Diff)
			diffTypesMap := make(map[string]Type)
			for _, diff := range diffs {
				fmt.Println(diff)
				diffsMap[diff.Name] = diff
				diffTypesMap[diff.Name] = diff.Type()
			}

			if tDiff := cmp.Diff(tc.expect, diffsMap); tDiff != "" {
				t.Fatal(tDiff)
			}

			if tDiff := cmp.Diff(tc.expectTypes, diffTypesMap); tDiff != "" {
				t.Fatal(tDiff)
			}
		})
	}
}
