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
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestClusterScoped(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "Object without metadata.namespace passes",
			obj:  fake.ClusterRole(),
		},
		{
			name:    "Object with metadata.namespace fails",
			obj:     fake.ClusterRole(core.Namespace("hello")),
			wantErr: nonhierarchical.IllegalNamespaceOnClusterScopedResourceError(fake.ClusterRole()),
		},
		{
			name: "Object with namespace selector fails",
			obj: fake.ClusterRole(
				core.Annotation(metadata.NamespaceSelectorAnnotationKey, "value")),
			wantErr: nonhierarchical.IllegalNamespaceSelectorAnnotationError(fake.ClusterRole()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ClusterScoped(tc.obj)
			if !errors.Is(errs, tc.wantErr) {
				t.Errorf("got ClusterScoped() error %v, want %v", errs, tc.wantErr)
			}
		})
	}
}

func TestClusterScopedForNamespaceReconciler(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name:    "Cluster scoped object fails",
			obj:     fake.ClusterRole(),
			wantErr: shouldBeInRootErr(fake.ClusterRole()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := ClusterScopedForNamespaceReconciler(tc.obj)
			if !errors.Is(errs, tc.wantErr) {
				t.Errorf("got ClusterScopedForNamespaceReconciler() error %v, want %v", errs, tc.wantErr)
			}
		})
	}
}

func TestNamespaceScoped(t *testing.T) {
	testCases := []struct {
		name    string
		obj     ast.FileObject
		wantErr status.Error
	}{
		{
			name: "Object without metadata.namespace passes",
			obj:  fake.Role(),
		},
		{
			name: "Object with metadata.namespace passes",
			obj:  fake.Role(core.Namespace("hello")),
		},
		{
			name: "Object with namespace selector passes",
			obj: fake.Role(
				core.Annotation(metadata.NamespaceSelectorAnnotationKey, "value")),
		},
		{
			name: "Object with namespace and namespace selector fails",
			obj: fake.Role(
				core.Namespace("hello"),
				core.Annotation(metadata.NamespaceSelectorAnnotationKey, "value")),
			wantErr: nonhierarchical.NamespaceAndSelectorResourceError(fake.Role()),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errs := NamespaceScoped(tc.obj)
			if !errors.Is(errs, tc.wantErr) {
				t.Errorf("got NamespaceScoped() error %v, want %v", errs, tc.wantErr)
			}
		})
	}
}
