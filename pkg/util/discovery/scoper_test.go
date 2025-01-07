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

package discovery

import (
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestScoper_GetScope(t *testing.T) {
	testCases := []struct {
		name      string
		scopes    map[schema.GroupKind]ScopeType
		groupKind schema.GroupKind
		want      ScopeType
		wantErr   status.Error
	}{
		{
			name:      "nil scoper returns Unknown and error",
			groupKind: kinds.Role().GroupKind(),
			want:      UnknownScope,
			wantErr:   status.UnknownGroupKindError(kinds.Namespace().GroupKind()),
		},
		{
			name:      "missing GroupKind returns unknown",
			scopes:    map[schema.GroupKind]ScopeType{},
			groupKind: kinds.Role().GroupKind(),
			want:      UnknownScope,
			wantErr:   status.UnknownGroupKindError(kinds.Namespace().GroupKind()),
		},
		{
			name: "NamespaceScope returns NamespaceScope",
			scopes: map[schema.GroupKind]ScopeType{
				kinds.Role().GroupKind(): NamespaceScope,
			},
			groupKind: kinds.Role().GroupKind(),
			want:      NamespaceScope,
		},
		{
			name: "ClusterScope returns ClusterScope",
			scopes: map[schema.GroupKind]ScopeType{
				kinds.Role().GroupKind(): ClusterScope,
			},
			groupKind: kinds.Role().GroupKind(),
			want:      ClusterScope,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scoper := Scoper{tc.scopes}
			got, gotErr := scoper.GetGroupKindScope(tc.groupKind)

			if got != tc.want {
				t.Errorf("got GetGroupKindScope() = %q, want %q", got, tc.want)
			}

			if !errors.Is(gotErr, tc.wantErr) {
				t.Errorf("got GetGroupKindScope() error = %v, want %v", gotErr, tc.wantErr)
			}
		})
	}
}

func crdV1Versions(versions []apiextensionsv1.CustomResourceDefinitionVersion) core.MetaMutator {
	return func(obj client.Object) {
		crd := obj.(*apiextensionsv1.CustomResourceDefinition)
		crd.Spec.Versions = versions
	}
}

func crdV1Scope(scope apiextensionsv1.ResourceScope) core.MetaMutator {
	return func(obj client.Object) {
		crd := obj.(*apiextensionsv1.CustomResourceDefinition)
		crd.Spec.Scope = scope
	}
}

func TestScopesFromCRD(t *testing.T) {
	namespaceScopedAnvilGKS := []GroupKindScope{{GroupKind: kinds.Anvil().GroupKind(), ScopeType: NamespaceScope}}
	clusterScopedAnvilGKS := []GroupKindScope{{GroupKind: kinds.Anvil().GroupKind(), ScopeType: ClusterScope}}

	testCases := []struct {
		name     string
		crd      *apiextensionsv1.CustomResourceDefinition
		expected []GroupKindScope
	}{
		// Trivial cases.
		{
			name: "no versions returns empty",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Versions(nil)),
		},
		// Test that scope is set correctly.
		{
			name:     "with version returns scope",
			crd:      k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped),
			expected: namespaceScopedAnvilGKS,
		},
		{
			name: "without scope defaults to Namespaced",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Scope("")),
			expected: namespaceScopedAnvilGKS,
		},
		{
			name:     "Cluster scope if specified",
			crd:      k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.ClusterScoped),
			expected: clusterScopedAnvilGKS,
		},
		// Served version conditions.
		{
			name: "with unserved version returns empty",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Versions([]apiextensionsv1.CustomResourceDefinitionVersion{
					{Name: "v1", Served: false},
				})),
		},
		{
			name: "with served version returns nonempty",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Versions([]apiextensionsv1.CustomResourceDefinitionVersion{
					{Name: "v1", Served: true},
				})),
			expected: namespaceScopedAnvilGKS,
		},
		{
			name: "with no served versions returns empty",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Versions([]apiextensionsv1.CustomResourceDefinitionVersion{
					{Name: "v1beta1", Served: false},
					{Name: "v1", Served: false},
				})),
		},
		{
			name: "with first version served returns nonempty",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Versions([]apiextensionsv1.CustomResourceDefinitionVersion{
					{Name: "v1beta1", Served: true},
					{Name: "v1", Served: false},
				})),
			expected: namespaceScopedAnvilGKS,
		},
		{
			name: "with second served version returns nonempty",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Versions([]apiextensionsv1.CustomResourceDefinitionVersion{
					{Name: "v1beta1", Served: false},
					{Name: "v1", Served: true},
				})),
			expected: namespaceScopedAnvilGKS,
		},
		{
			name: "with two served versions returns nonempty",
			crd: k8sobjects.CRDV1ObjectForGVK(kinds.Anvil(), apiextensionsv1.NamespaceScoped,
				crdV1Versions([]apiextensionsv1.CustomResourceDefinitionVersion{
					{Name: "v1beta1", Served: true},
					{Name: "v1", Served: true},
				})),
			expected: namespaceScopedAnvilGKS,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := scopesFromCRDs([]*apiextensionsv1.CustomResourceDefinition{tc.crd})

			if diff := cmp.Diff(tc.expected, actual); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}

// TestScopesFromCRDv1beta1 validates that a v1beta1.CustomResourceDefinition
// converted to a v1.CustomResourceDefinition can still be used with
// scopesFromCRDs. This confirms that scheme-based conversion handles converting
// v1beta1 spec.version field to the newer spec.versions list, and sets
// served=true.
func TestScopesFromCRDv1beta1(t *testing.T) {
	crdV1beta1 := k8sobjects.CRDV1Beta1ObjectForGVK(kinds.Anvil(), apiextensionsv1beta1.NamespaceScoped)
	crdV1Unstructured, err := kinds.ToTypedWithVersion(crdV1beta1, kinds.CustomResourceDefinitionV1(), core.Scheme)
	require.NoError(t, err)
	crdV1 := crdV1Unstructured.(*apiextensionsv1.CustomResourceDefinition)
	actual := scopesFromCRDs([]*apiextensionsv1.CustomResourceDefinition{crdV1})
	expected := []GroupKindScope{{GroupKind: kinds.Anvil().GroupKind(), ScopeType: NamespaceScope}}
	require.Equal(t, expected, actual)
}

func TestNilScoper(t *testing.T) {
	scoper := Scoper{}

	scoper.SetGroupKindScope(kinds.Namespace().GroupKind(), ClusterScope)

	got, gotErr := scoper.GetGroupKindScope(kinds.Namespace().GroupKind())

	if got != ClusterScope {
		t.Errorf("got GetGroupKindScope() = %v, want %v", ClusterScope, got)
	}

	if gotErr != nil {
		t.Errorf("got GetGroupKindSCope() err = %v, want nil", gotErr)
	}
}
