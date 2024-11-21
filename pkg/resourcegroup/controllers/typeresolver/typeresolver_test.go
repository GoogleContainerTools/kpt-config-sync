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

package typeresolver

import (
	"fmt"
	"testing"

	"github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"kpt.dev/configsync/pkg/testing/testerrors"
)

func TestResolve(t *testing.T) {
	r := fakeResolver()
	tests := map[string]struct {
		input         schema.GroupKind
		expectFound   bool
		expectVersion string
	}{
		"non existing type return false": {
			input:       schema.GroupKind{Group: "not.exist", Kind: "UnFound"},
			expectFound: false,
		},
		"should have ConfigMap": {
			input:         schema.GroupKind{Group: "", Kind: "ConfigMap"},
			expectFound:   true,
			expectVersion: "v1",
		},
		"should have Deployment": {
			input:         schema.GroupKind{Group: "apps", Kind: "Deployment"},
			expectFound:   true,
			expectVersion: "v1",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			gvk, found := r.Resolve(tc.input)
			assert.Equal(t, tc.expectFound, found)
			assert.Equal(t, tc.expectVersion, gvk.Version)
		})
	}
}

// TestRefresh tests that TypeResolver.Refresh tolerates ErrGroupDiscoveryFailed
// errors, but only if all the wrapped group errors are StaleGroupVersionError.
// All the discovered resources should then be resolvable to the version
// preferred by the server with TypeResolver.Resolve.
func TestRefresh(t *testing.T) {
	coreGV := schema.GroupVersion{
		Group:   "core",
		Version: "v1",
	}
	coreAPIGroup := metav1.APIGroup{
		Name: coreGV.Group,
		Versions: []metav1.GroupVersionForDiscovery{
			{
				GroupVersion: coreGV.String(),
				Version:      coreGV.Version,
			},
		},
		PreferredVersion: metav1.GroupVersionForDiscovery{
			GroupVersion: coreGV.String(),
			Version:      coreGV.Version,
		},
		ServerAddressByClientCIDRs: []metav1.ServerAddressByClientCIDR{}, // unused
	}
	nsGVK := schema.GroupVersionKind{
		Group:   coreGV.Group,
		Version: coreGV.Version,
		Kind:    "Namespace",
	}
	nsAPIResource := metav1.APIResource{
		Name:         "namespaces",
		SingularName: "namespace",
		Kind:         nsGVK.Kind,
		Group:        nsGVK.Group,
		Version:      nsGVK.Version,
	}
	anvilGVK := schema.GroupVersionKind{
		Group:   "acme.com",
		Version: "v1",
		Kind:    "Anvil",
	}
	tests := []struct {
		name                     string
		setupFakeDiscoveryClient func(*FakeDiscoveryClient)
		expectedError            error
		expectedGVKs             map[schema.GroupKind]schema.GroupVersionKind
	}{
		{
			name: "all groups available from aggregated resource request",
			setupFakeDiscoveryClient: func(fakeDC *FakeDiscoveryClient) {
				fakeDC.GroupsAndMaybeResourcesOutputs = []FakeDiscoveryGroupsAndMaybeResourcesOutput{
					{
						Groups: &metav1.APIGroupList{
							Groups: []metav1.APIGroup{
								coreAPIGroup,
							},
						},
						Resources: map[schema.GroupVersion]*metav1.APIResourceList{
							coreGV: {
								APIResources: []metav1.APIResource{
									nsAPIResource,
								},
							},
						},
						FailedGroupVersions: nil,
						Error:               nil,
					},
				}
			},
			expectedError: nil,
			expectedGVKs: map[schema.GroupKind]schema.GroupVersionKind{
				nsGVK.GroupKind():    nsGVK,
				anvilGVK.GroupKind(): {}, // expect not found
			},
		},
		{
			name: "all groups available from legacy resource requests",
			setupFakeDiscoveryClient: func(fakeDC *FakeDiscoveryClient) {
				fakeDC.GroupsAndMaybeResourcesOutputs = []FakeDiscoveryGroupsAndMaybeResourcesOutput{
					{
						Groups: &metav1.APIGroupList{
							Groups: []metav1.APIGroup{
								coreAPIGroup,
							},
						},
						Resources:           nil, // must be nil to trigger ServerResourcesForGroupVersion call
						FailedGroupVersions: nil,
						Error:               nil,
					},
				}
				fakeDC.ServerResourcesForGroupVersionOutputs = []FakeDiscoveryServerResourcesForGroupVersionOutput{
					{
						Resources: &metav1.APIResourceList{
							APIResources: []metav1.APIResource{
								nsAPIResource,
							},
						},
						Error: nil,
					},
				}
			},
			expectedError: nil,
			expectedGVKs: map[schema.GroupKind]schema.GroupVersionKind{
				nsGVK.GroupKind():    nsGVK,
				anvilGVK.GroupKind(): {}, // expect not found
			},
		},
		{
			name: "group error with one stale group",
			setupFakeDiscoveryClient: func(fakeDC *FakeDiscoveryClient) {
				fakeDC.GroupsAndMaybeResourcesOutputs = []FakeDiscoveryGroupsAndMaybeResourcesOutput{
					{
						Groups: &metav1.APIGroupList{
							Groups: []metav1.APIGroup{
								coreAPIGroup,
							},
						},
						Resources: map[schema.GroupVersion]*metav1.APIResourceList{
							coreGV: {
								APIResources: []metav1.APIResource{
									nsAPIResource,
								},
							},
						},
						FailedGroupVersions: map[schema.GroupVersion]error{
							anvilGVK.GroupVersion(): discovery.StaleGroupVersionError{},
						},
						Error: nil,
					},
				}
			},
			expectedError: nil,
			expectedGVKs: map[schema.GroupKind]schema.GroupVersionKind{
				nsGVK.GroupKind():    nsGVK,
				anvilGVK.GroupKind(): {}, // expect not found
			},
		},
		{
			name: "group error with one other error",
			setupFakeDiscoveryClient: func(fakeDC *FakeDiscoveryClient) {
				fakeDC.GroupsAndMaybeResourcesOutputs = []FakeDiscoveryGroupsAndMaybeResourcesOutput{
					{
						Groups: &metav1.APIGroupList{},
						// Resources must be non-nil to avoid ServerResourcesForGroupVersion call
						Resources: map[schema.GroupVersion]*metav1.APIResourceList{},
						FailedGroupVersions: map[schema.GroupVersion]error{
							coreGV: fmt.Errorf("other error"),
						},
						Error: nil,
					},
				}
			},
			expectedError: fmt.Errorf("discovery of API resources failed: %w",
				&discovery.ErrGroupDiscoveryFailed{
					Groups: map[schema.GroupVersion]error{
						coreGV: fmt.Errorf("other error"),
					},
				}),
			expectedGVKs: map[schema.GroupKind]schema.GroupVersionKind{
				nsGVK.GroupKind():    {}, // expect not found
				anvilGVK.GroupKind(): {}, // expect not found
			},
		},
		{
			name: "group error with one stale and one other error",
			setupFakeDiscoveryClient: func(fakeDC *FakeDiscoveryClient) {
				fakeDC.GroupsAndMaybeResourcesOutputs = []FakeDiscoveryGroupsAndMaybeResourcesOutput{
					{
						Groups: &metav1.APIGroupList{},
						// Resources must be non-nil to avoid ServerResourcesForGroupVersion call
						Resources: map[schema.GroupVersion]*metav1.APIResourceList{},
						FailedGroupVersions: map[schema.GroupVersion]error{
							coreGV:                  fmt.Errorf("other error"),
							anvilGVK.GroupVersion(): discovery.StaleGroupVersionError{},
						},
						Error: nil,
					},
				}
			},
			expectedError: fmt.Errorf("discovery of API resources failed: %w",
				&discovery.ErrGroupDiscoveryFailed{
					Groups: map[schema.GroupVersion]error{
						coreGV:                  fmt.Errorf("other error"),
						anvilGVK.GroupVersion(): discovery.StaleGroupVersionError{},
					},
				}),
			expectedGVKs: map[schema.GroupKind]schema.GroupVersionKind{
				nsGVK.GroupKind():    {}, // expect not found
				anvilGVK.GroupKind(): {}, // expect not found
			},
		},
		{
			name: "non-group error",
			setupFakeDiscoveryClient: func(fakeDC *FakeDiscoveryClient) {
				fakeDC.GroupsAndMaybeResourcesOutputs = []FakeDiscoveryGroupsAndMaybeResourcesOutput{
					{
						Groups: &metav1.APIGroupList{},
						// Resources must be non-nil to avoid ServerResourcesForGroupVersion call
						Resources:           map[schema.GroupVersion]*metav1.APIResourceList{},
						FailedGroupVersions: nil,
						Error:               fmt.Errorf("other error"),
					},
				}
			},
			expectedError: fmt.Errorf("discovery of API resources failed: %w",
				fmt.Errorf("other error")),
			expectedGVKs: map[schema.GroupKind]schema.GroupVersionKind{
				nsGVK.GroupKind():    {}, // expect not found
				anvilGVK.GroupKind(): {}, // expect not found
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fakeDC := &FakeDiscoveryClient{}
			if tc.setupFakeDiscoveryClient != nil {
				tc.setupFakeDiscoveryClient(fakeDC)
			}
			logger := testr.New(t)
			resolver := NewTypeResolver(fakeDC, logger)
			err := resolver.Refresh()
			testerrors.AssertEqual(t, err, tc.expectedError)
			require.Equal(t, fakeDC.ServerResourcesForGroupVersionCalls, len(fakeDC.ServerResourcesForGroupVersionInputs))

			for gk, expectedGVK := range tc.expectedGVKs {
				gvk, found := resolver.Resolve(gk)
				assert.Equal(t, !expectedGVK.Empty(), found)
				if found {
					assert.Equal(t, expectedGVK, gvk)
				}
			}
		})
	}
}
