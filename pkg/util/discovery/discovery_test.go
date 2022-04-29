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
	"testing"

	"k8s.io/apimachinery/pkg/runtime/schema"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"

	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestAPIInfo_GroupVersionKinds(t *testing.T) {
	testCases := []struct {
		name          string
		resourceLists []*metav1.APIResourceList
		syncs         []*v1.Sync
		expected      map[schema.GroupVersionKind]bool
	}{
		{
			name: "Lists only mentioned gks",
			resourceLists: []*metav1.APIResourceList{
				{
					GroupVersion: "rbac/v1",
					APIResources: []metav1.APIResource{
						{
							Kind: "Role",
						},
					},
				},
				{
					GroupVersion: "rbac/v2",
					APIResources: []metav1.APIResource{
						{
							Kind: "Role",
						},
					},
				},
				{
					GroupVersion: "apps/v1",
					APIResources: []metav1.APIResource{
						{
							Kind: "Deployment",
						},
					},
				},
			},
			syncs: []*v1.Sync{
				{
					Spec: v1.SyncSpec{
						Group: "rbac",
						Kind:  "Role",
					},
				},
			},
			expected: map[schema.GroupVersionKind]bool{
				{Group: "rbac", Version: "v1", Kind: "Role"}: true,
				{Group: "rbac", Version: "v2", Kind: "Role"}: true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			api, err := NewAPIInfo(tc.resourceLists)
			if err != nil {
				t.Error(err)
			}

			result := api.GroupVersionKinds(tc.syncs...)
			if diff := cmp.Diff(tc.expected, result); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
