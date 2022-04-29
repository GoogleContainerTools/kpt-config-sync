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

package scheme

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestResourceScopes(t *testing.T) {
	testCases := []struct {
		name                string
		gvks                map[schema.GroupVersionKind]bool
		wantNamespacedTypes map[schema.GroupVersionKind]client.Object
		wantClusterTypes    map[schema.GroupVersionKind]client.Object
		wantErr             bool
	}{
		{
			name: "Ignore CustomResourceDefinition v1beta1",
			gvks: map[schema.GroupVersionKind]bool{
				kinds.CustomResourceDefinitionV1Beta1(): true,
			},
		},
		{
			name: "Ignore CustomResourceDefinition v1",
			gvks: map[schema.GroupVersionKind]bool{
				kinds.CustomResourceDefinitionV1Beta1().GroupKind().WithVersion("v1"): true,
			},
		},
		{
			name: "ClusterRole is cluster-scoped",
			gvks: map[schema.GroupVersionKind]bool{
				kinds.ClusterRole(): true,
			},
			wantClusterTypes: map[schema.GroupVersionKind]client.Object{
				kinds.ClusterRole(): &rbacv1.ClusterRole{},
			},
		},
		{
			name: "Role is namespaced",
			gvks: map[schema.GroupVersionKind]bool{
				kinds.Role(): true,
			},
			wantNamespacedTypes: map[schema.GroupVersionKind]client.Object{
				kinds.Role(): &rbacv1.Role{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			s := runtime.NewScheme()
			s.AddKnownTypeWithName(kinds.CustomResourceDefinitionV1Beta1(), &v1beta1.CustomResourceDefinition{})
			s.AddKnownTypeWithName(kinds.CustomResourceDefinitionV1Beta1().GroupKind().WithVersion("v1"), &unstructured.Unstructured{})
			err := rbacv1.AddToScheme(s)
			if err != nil {
				t.Fatal(err)
			}

			ns, cluster, err := ResourceScopes(tc.gvks, s, discovery.CoreScoper())

			if diff := cmp.Diff(tc.wantNamespacedTypes, ns, cmpopts.EquateEmpty()); diff != "" {
				t.Error(diff)
			}
			if diff := cmp.Diff(tc.wantClusterTypes, cluster, cmpopts.EquateEmpty()); diff != "" {
				t.Error(diff)
			}
			if err != nil && !tc.wantErr {
				t.Errorf("got error %s, want nil error", err)
			} else if err == nil && tc.wantErr {
				t.Error("got nil error, want error")
			}
		})
	}
}
