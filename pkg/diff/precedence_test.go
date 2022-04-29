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

package diff

import (
	"testing"

	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff/difftest"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const rsName = "test-rs"

func TestCanManage(t *testing.T) {
	testCases := []struct {
		name   string
		scope  declared.Scope
		object client.Object
		want   bool
	}{
		{
			"Root can manage unmanaged object",
			declared.RootReconciler,
			fake.DeploymentObject(),
			true,
		},
		{
			"Root can manage any non-root-managed object",
			declared.RootReconciler,
			fake.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy("foo", "any-rs")),
			true,
		},
		{
			"Root can manage self-managed object",
			declared.RootReconciler,
			fake.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy(declared.RootReconciler, rsName)),
			true,
		},
		{
			"Root can NOT manage other root-managed object",
			declared.RootReconciler,
			fake.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy(declared.RootReconciler, "other-rs")),
			false,
		},
		{
			"Root can manage seemingly other root-managed object",
			declared.RootReconciler,
			fake.DeploymentObject(difftest.ManagedBy(declared.RootReconciler, "other-rs")),
			true,
		},
		{
			"Non-root can manage unmanaged object",
			"foo",
			fake.DeploymentObject(),
			true,
		},
		{
			"Non-root can manage self-managed object",
			"foo",
			fake.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy("foo", rsName)),
			true,
		},
		{
			"Non-root can NOT manage other non-root-managed object",
			"foo",
			fake.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy("foo", "other-rs")),
			false,
		},
		{
			"Non-root can NOT manage root-managed object",
			"foo",
			fake.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy(declared.RootReconciler, "any-rs")),
			false,
		},
		{
			"Non-root can manage seemingly other non-root-managed object",
			"foo",
			fake.DeploymentObject(difftest.ManagedBy("foo", "other-rs")),
			true,
		},
		{
			"Non-root can manage seemingly root-managed object",
			"foo",
			fake.DeploymentObject(difftest.ManagedBy(declared.RootReconciler, "any-rs")),
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanManage(tc.scope, rsName, tc.object)
			if got != tc.want {
				t.Errorf("CanManage() = %v; want %v", got, tc.want)
			}
		})
	}
}
