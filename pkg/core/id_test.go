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

package core_test

import (
	"testing"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestGKNN(t *testing.T) {
	testcases := []struct {
		name string
		obj  client.Object
		want string
	}{
		{
			name: "a namespaced object",
			obj:  fake.RoleObject(core.Namespace("test")),
			want: "rbac.authorization.k8s.io_role_test_default-name",
		},
		{
			name: "a cluster-scoped object",
			obj:  fake.ClusterRoleObject(),
			want: "rbac.authorization.k8s.io_clusterrole_default-name",
		},
		{
			name: "a nil object",
			obj:  nil,
			want: "",
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			got := core.GKNN(tc.obj)
			if tc.want != got {
				t.Errorf("GKNN() = %q, got %q", got, tc.want)
			}
		})
	}
}
