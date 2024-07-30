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
	"fmt"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff/difftest"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"kpt.dev/configsync/pkg/testing/testerrors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const rsName = "test-rs"

func TestCanManage(t *testing.T) {
	testCases := []struct {
		name      string
		scope     declared.Scope
		object    client.Object
		operation admissionv1.Operation
		want      bool
	}{
		{
			"Root can manage unmanaged object",
			declared.RootScope,
			k8sobjects.DeploymentObject(),
			OperationManage,
			true,
		},
		{
			"Root can manage any non-root-managed object",
			declared.RootScope,
			k8sobjects.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy("foo", "any-rs")),
			OperationManage,
			true,
		},
		{
			"Root can manage self-managed object",
			declared.RootScope,
			k8sobjects.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy(declared.RootScope, rsName)),
			OperationManage,
			true,
		},
		{
			"Root can NOT manage other root-managed object",
			declared.RootScope,
			k8sobjects.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy(declared.RootScope, "other-rs")),
			OperationManage,
			false,
		},
		{
			"Root can manage seemingly other root-managed object",
			declared.RootScope,
			k8sobjects.DeploymentObject(difftest.ManagedBy(declared.RootScope, "other-rs")),
			OperationManage,
			true,
		},
		{
			"Non-root can manage unmanaged object",
			"foo",
			k8sobjects.DeploymentObject(),
			OperationManage,
			true,
		},
		{
			"Non-root can manage self-managed object",
			"foo",
			k8sobjects.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy("foo", rsName)),
			OperationManage,
			true,
		},
		{
			"Non-root can NOT manage other non-root-managed object",
			"foo",
			k8sobjects.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy("foo", "other-rs")),
			OperationManage,
			false,
		},
		{
			"Non-root can NOT manage root-managed object",
			"foo",
			k8sobjects.DeploymentObject(syncertest.ManagementEnabled, difftest.ManagedBy(declared.RootScope, "any-rs")),
			OperationManage,
			false,
		},
		{
			"Non-root can manage seemingly other non-root-managed object",
			"foo",
			k8sobjects.DeploymentObject(difftest.ManagedBy("foo", "other-rs")),
			OperationManage,
			true,
		},
		{
			"Non-root can manage seemingly root-managed object",
			"foo",
			k8sobjects.DeploymentObject(difftest.ManagedBy(declared.RootScope, "any-rs")),
			OperationManage,
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanManage(tc.scope, rsName, tc.object, tc.operation)
			if got != tc.want {
				t.Errorf("CanManage() = %v; want %v", got, tc.want)
			}
		})
	}
}

func TestValidateManager(t *testing.T) {
	cmID := core.ID{
		GroupKind: schema.GroupKind{Group: "example.com", Kind: "ConfigMap"},
		ObjectKey: client.ObjectKey{Namespace: "ns-1", Name: "cm-1"},
	}
	rootSyncID := core.ID{
		GroupKind: schema.GroupKind{Group: configsync.GroupName, Kind: configsync.RootSyncKind},
		ObjectKey: client.ObjectKey{Namespace: configsync.ControllerNamespace, Name: "rootsync-1"},
	}
	repoSyncID := core.ID{
		GroupKind: schema.GroupKind{Group: configsync.GroupName, Kind: configsync.RepoSyncKind},
		ObjectKey: client.ObjectKey{Namespace: "ns-1", Name: "reposync-1"},
	}
	testCases := []struct {
		name       string
		reconciler string
		manager    string
		id         core.ID
		operation  admissionv1.Operation
		want       error
	}{
		{
			name:       "Root reconciler can manage its own object",
			reconciler: "root-reconciler",
			manager:    ":root",
			want:       nil,
		},
		{
			name:       "Root reconciler can manage object with any namespace manager",
			reconciler: "root-reconciler",
			manager:    "bookstore",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "Root reconciler can update its own RootSync",
			reconciler: core.RootReconcilerName(rootSyncID.Name),
			manager:    "any-other-manager",
			id:         rootSyncID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "Root reconciler can not create its own RootSync",
			reconciler: core.RootReconcilerName(rootSyncID.Name),
			manager:    "any-other-manager",
			id:         rootSyncID,
			operation:  admissionv1.Create,
			want:       fmt.Errorf(`config sync "root-reconciler-rootsync-1" can not CREATE object "RootSync.configsync.gke.io, config-management-system/rootsync-1" managed by config sync "ns-reconciler-any-other-manager"`),
		},
		{
			name:       "Root reconciler can not delete its own RootSync",
			reconciler: core.RootReconcilerName(rootSyncID.Name),
			manager:    "any-other-manager",
			id:         rootSyncID,
			operation:  admissionv1.Delete,
			want:       fmt.Errorf(`config sync "root-reconciler-rootsync-1" can not DELETE object "RootSync.configsync.gke.io, config-management-system/rootsync-1" managed by config sync "ns-reconciler-any-other-manager"`),
		},
		{
			name:       "Root reconciler can not manage object with other root manager",
			reconciler: "root-reconciler",
			manager:    ":root_test-rs",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       fmt.Errorf(`config sync "root-reconciler" can not UPDATE object "ConfigMap.example.com, ns-1/cm-1" managed by config sync "root-reconciler-test-rs"`),
		},
		{
			name:       "Root reconciler can manage object with no manager",
			reconciler: "root-reconciler",
			manager:    "",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "Namespace reconciler can manage its own object",
			reconciler: "ns-reconciler-bookstore",
			manager:    "bookstore",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "Namespace reconciler can update its own RepoSync",
			reconciler: core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name),
			manager:    "any-other-manager",
			id:         repoSyncID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "Namespace reconciler can not create its own RepoSync",
			reconciler: core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name),
			manager:    "any-other-manager",
			id:         repoSyncID,
			operation:  admissionv1.Create,
			want:       fmt.Errorf(`config sync "ns-reconciler-ns-1-reposync-1-10" can not CREATE object "RepoSync.configsync.gke.io, ns-1/reposync-1" managed by config sync "ns-reconciler-any-other-manager"`),
		},
		{
			name:       "Namespace reconciler can not delete its own RepoSync",
			reconciler: core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name),
			manager:    "any-other-manager",
			id:         repoSyncID,
			operation:  admissionv1.Delete,
			want:       fmt.Errorf(`config sync "ns-reconciler-ns-1-reposync-1-10" can not DELETE object "RepoSync.configsync.gke.io, ns-1/reposync-1" managed by config sync "ns-reconciler-any-other-manager"`),
		},
		{
			name:       "Namespace reconciler can not manage object with manager in different namespace",
			reconciler: "ns-reconciler-bookstore",
			manager:    "videostore",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       fmt.Errorf(`config sync "ns-reconciler-bookstore" can not UPDATE object "ConfigMap.example.com, ns-1/cm-1" managed by config sync "ns-reconciler-videostore"`),
		},
		{
			name:       "Namespace reconciler can not manage object with different manager in the same namespace",
			reconciler: "ns-reconciler-bookstore",
			manager:    "bookstore_test-rs",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       fmt.Errorf(`config sync "ns-reconciler-bookstore" can not UPDATE object "ConfigMap.example.com, ns-1/cm-1" managed by config sync "ns-reconciler-bookstore-test-rs-7"`),
		},
		{
			name:       "Namespace reconciler can not manage object with any root manager",
			reconciler: "ns-reconciler-bookstore",
			manager:    ":root",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       fmt.Errorf(`config sync "ns-reconciler-bookstore" can not UPDATE object "ConfigMap.example.com, ns-1/cm-1" managed by config sync "root-reconciler"`),
		},
		{
			name:       "Namespace reconciler can manage object with no manager",
			reconciler: "ns-reconciler-bookstore",
			manager:    "",
			id:         cmID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "ReconcilerManager can update RootSync with a manager",
			reconciler: "reconciler-manager",
			manager:    "bookstore",
			id:         rootSyncID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "ReconcilerManager can update RepoSync with a manager",
			reconciler: "reconciler-manager",
			manager:    "bookstore",
			id:         repoSyncID,
			operation:  admissionv1.Update,
			want:       nil,
		},
		{
			name:       "ReconcilerManager can not create RootSync with a manager",
			reconciler: "reconciler-manager",
			manager:    "bookstore",
			id:         rootSyncID,
			operation:  admissionv1.Create,
			want:       fmt.Errorf(`config sync "reconciler-manager" can not CREATE object "RootSync.configsync.gke.io, config-management-system/rootsync-1" managed by config sync "ns-reconciler-bookstore"`),
		},
		{
			name:       "ReconcilerManager can not create RepoSync with a manager",
			reconciler: "reconciler-manager",
			manager:    "bookstore",
			id:         repoSyncID,
			operation:  admissionv1.Create,
			want:       fmt.Errorf(`config sync "reconciler-manager" can not CREATE object "RepoSync.configsync.gke.io, ns-1/reposync-1" managed by config sync "ns-reconciler-bookstore"`),
		},
		{
			name:       "ReconcilerManager can not delete RootSync with a manager",
			reconciler: "reconciler-manager",
			manager:    "bookstore",
			id:         rootSyncID,
			operation:  admissionv1.Delete,
			want:       fmt.Errorf(`config sync "reconciler-manager" can not DELETE object "RootSync.configsync.gke.io, config-management-system/rootsync-1" managed by config sync "ns-reconciler-bookstore"`),
		},
		{
			name:       "ReconcilerManager can not delete RepoSync with a manager",
			reconciler: "reconciler-manager",
			manager:    "bookstore",
			id:         repoSyncID,
			operation:  admissionv1.Delete,
			want:       fmt.Errorf(`config sync "reconciler-manager" can not DELETE object "RepoSync.configsync.gke.io, ns-1/reposync-1" managed by config sync "ns-reconciler-bookstore"`),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateManager(tc.reconciler, tc.manager, tc.id, tc.operation)
			testerrors.AssertEqual(t, tc.want, got)
		})
	}
}

func TestIsRootReconciler(t *testing.T) {
	testCases := []struct {
		name           string
		reconcilerName string
		want           bool
	}{
		{
			name:           "root reconciler",
			reconcilerName: "root-reconciler",
			want:           true,
		},
		{
			name:           "root reconciler with sync name",
			reconcilerName: "root-reconciler-config-2",
			want:           true,
		},
		{
			name:           "monitor",
			reconcilerName: "monitor",
			want:           false,
		},
		{
			name:           "namespace reconciler",
			reconcilerName: "ns-reconciler-namespace-name-4",
			want:           false,
		},
		{
			name:           "empty",
			reconcilerName: "",
			want:           false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRootReconciler(tc.reconcilerName); got != tc.want {
				t.Errorf("isRootReconciler() got %v; want %v", got, tc.want)
			}
		})
	}
}
