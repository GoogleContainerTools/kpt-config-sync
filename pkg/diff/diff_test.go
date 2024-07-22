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

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff/difftest"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/policycontroller"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testNs1  = "fake-namespace-1"
	testNs2  = "fake-namespace-2"
	syncName = "rs"
)

func TestDiffType(t *testing.T) {
	testCases := []struct {
		name     string
		scope    declared.Scope
		syncName string
		declared client.Object
		actual   client.Object
		want     Operation
	}{
		// Declared + no actual paths.
		{
			name:     "declared + no actual, management enabled: create",
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			want:     Create,
		},
		{
			name:     "declared + no actual, management disabled: no op",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled),
			want:     NoOp,
		},
		{
			name:     "declared + no actual, no management: error",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(),
			want:     Error,
		},
		// Declared + actual paths.
		{
			name:     "declared + actual, management enabled, no manager annotation, root scope, can manage: update",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual:   k8sobjects.RoleObject(syncertest.ManagementEnabled),
			want:     Update,
		},
		{
			name:     "declared + actual, management enabled, no manager annotation, namespace scope, can manage: update",
			scope:    "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual:   k8sobjects.RoleObject(syncertest.ManagementEnabled),
			want:     Update,
		},
		{
			name:     "declared + actual, management enabled, root scope / self-owned object, can manage: update",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				difftest.ManagedBy(declared.RootScope, syncName)),
			want: Update,
		},
		{
			name:     "declared + actual, management enabled, root scope / namespace-owned object, can manage: update",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				difftest.ManagedBy("shipping", "any-rs")),
			want: Update,
		},
		{
			name:     "declared + actual, management enabled, namespace scope / self-owned object, can manage: update",
			scope:    "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				difftest.ManagedBy("shipping", syncName)),
			want: Update,
		},
		{
			name:     "declared + actual, management enabled, root scope / other root-owned object, can manage: conflict",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				difftest.ManagedBy(declared.RootScope, "other-rs")),
			want: ManagementConflict,
		},
		{
			name:     "declared + actual, management enabled, namespace scope / other namespace-owned object, can manage: conflict",
			scope:    "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				difftest.ManagedBy("shipping", "other-rs")),
			want: ManagementConflict,
		},
		{
			name:  "declared + actual, management enabled, namespace scope / root-owned object: conflict",
			scope: "foo",
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("foo")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("foo"),
				difftest.ManagedBy(declared.RootScope, "any-rs")),
			want: ManagementConflict,
		},
		{
			name:     "declared + actual, management disabled, root scope, can manage, with meta, no manager: abandon",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled),
			actual:   k8sobjects.RoleObject(syncertest.ManagementEnabled),
			want:     Abandon,
		},
		{
			name:     "declared + actual, management disabled, namespace scope, can manage, with meta, no manager: abandon",
			scope:    "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled),
			actual:   k8sobjects.RoleObject(syncertest.ManagementEnabled),
			want:     Abandon,
		},
		{
			name:     "declared + actual, management disabled, root scope, can manage, no meta: no op",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled),
			actual:   k8sobjects.RoleObject(),
			want:     NoOp,
		},
		{
			name:     "declared + actual, management disabled, namespace scope, can manage, no meta: no op",
			scope:    "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled),
			actual:   k8sobjects.RoleObject(),
			want:     NoOp,
		},
		{
			name:  "declared + actual, management disabled, namespace scope / root-owned object: no op",
			scope: "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				core.Namespace("shipping")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("shipping"),
				difftest.ManagedBy(declared.RootScope, "any-rs")),
			want: NoOp,
		},
		{
			name:  "declared + actual, management disabled, namespace scope / self-owned object: abandon",
			scope: "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				core.Namespace("shipping")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("shipping"),
				difftest.ManagedBy("shipping", syncName)),
			want: Abandon,
		},
		{
			name:  "declared + actual, management disabled, namespace scope / other namespace-owned object: no op",
			scope: "shipping",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				core.Namespace("shipping")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("shipping"),
				difftest.ManagedBy("shipping", "other-rs")),
			want: NoOp,
		},
		{
			name: "declared + actual, management disabled, root scope / namespace-owned object: no op",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				core.Namespace("shipping")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("shipping"),
				difftest.ManagedBy("shipping", "any-rs")),
			want: NoOp,
		},
		{
			name: "declared + actual, management disabled, root scope / self-owned object: abandon",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				core.Namespace("shipping")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("shipping"),
				difftest.ManagedBy(declared.RootScope, syncName)),
			want: Abandon,
		},
		{
			name: "declared + actual, management disabled, root scope / other root-owned object: no op",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				core.Namespace("shipping")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("shipping"),
				difftest.ManagedBy(declared.RootScope, "other-rs")),
			want: NoOp,
		},
		{
			name: "declared + actual, management disabled, root scope / empty management annotation object: abandon",
			declared: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				core.Namespace("shipping")),
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Namespace("shipping"),
				difftest.ManagedBy("", configsync.RepoSyncName)),
			want: Abandon,
		},
		{
			name:     "declared + actual, declared management invalid: error",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementInvalid),
			actual:   k8sobjects.RoleObject(),
			want:     Error,
		},
		{
			name:     "declared + actual, actual management invalid: error",
			scope:    declared.RootScope,
			declared: k8sobjects.RoleObject(syncertest.ManagementEnabled),
			actual:   k8sobjects.RoleObject(syncertest.ManagementInvalid),
			want:     Update,
		},
		// IgnoreMutation path.
		{
			name: "prevent mutations",
			declared: k8sobjects.RoleObject(
				syncertest.ManagementEnabled,
				core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
				core.Annotation("foo", "bar"),
			),
			actual: k8sobjects.RoleObject(
				syncertest.ManagementEnabled,
				core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
				core.Annotation("foo", "qux"),
			),
			want: NoOp,
		},
		{
			name: "update if actual missing annotation",
			// The use case where the user has added the annotation to an object. We
			// need to update the object so the actual one has the annotation now.
			declared: k8sobjects.RoleObject(
				syncertest.ManagementEnabled,
				core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
				core.Annotation("foo", "bar"),
			),
			actual: k8sobjects.RoleObject(
				syncertest.ManagementEnabled,
				core.Annotation("foo", "qux"),
			),
			want: Update,
		},
		{
			name: "update if declared missing annotation",
			// This corresponds to the use case where the user has removed the
			// annotation, indicating they want us to begin updating the object again.
			//
			// There is an edge case where users manually annotate in-cluster objects,
			// which has no effect on our behavior; we only honor declared lifecycle
			// annotations.
			declared: k8sobjects.RoleObject(
				syncertest.ManagementEnabled,
				core.Annotation("foo", "bar"),
			),
			actual: k8sobjects.RoleObject(
				core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
				core.Annotation("foo", "qux"),
			),
			want: Update,
		},
		// Actual + no declared paths.
		{
			name:   "actual + no declared, no meta: no-op",
			scope:  declared.RootScope,
			actual: k8sobjects.RoleObject(),
			want:   NoOp,
		},
		{
			name:  "actual + no declared, owned: noop",
			scope: declared.RootScope,
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.OwnerReference([]metav1.OwnerReference{
					{},
				})),
			want: NoOp,
		},
		{
			name:  "actual + no declared, cannot manage (actual is managed by Config Sync): noop",
			scope: "shipping",
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_default-name"),
				difftest.ManagedBy(declared.RootScope, configsync.RootSyncName)),
			want: NoOp,
		},
		{
			name:  "actual + no declared, cannot manage (actual is not managed by Config Sync): noop",
			scope: "shipping",
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_wrong-name"),
				difftest.ManagedBy(declared.RootScope, configsync.RootSyncName)),
			want: NoOp,
		},
		{
			name:  "actual + no declared, not managed by Config Sync but has other Config Sync annotations: noop",
			scope: "shipping",
			actual: k8sobjects.RoleObject(syncertest.TokenAnnotation,
				difftest.ManagedBy(declared.RootScope, configsync.RootSyncName)),
			want: NoOp,
		},
		{
			name:  "actual + no declared, not managed by Config Sync (the configsync.gke.io/resource-id annotation is unset): noop",
			scope: "shipping",
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				difftest.ManagedBy(declared.RootScope, configsync.RootSyncName)),
			want: NoOp,
		},
		{
			name:  "actual + no declared, not managed by Config Sync (the configmanagement.gke.io/managed annotation is set to disabled): noop",
			scope: "shipping",
			actual: k8sobjects.RoleObject(syncertest.ManagementDisabled,
				difftest.ManagedBy(declared.RootScope, configsync.RootSyncName)),
			want: NoOp,
		},
		{
			name:  "actual + no declared, not managed by Config Sync (the configsync.gke.io/resource-id annotation is incorrect): noop",
			scope: "shipping",
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_wrong-name"),
				difftest.ManagedBy(declared.RootScope, configsync.RootSyncName)),
			want: NoOp,
		},
		{
			name: "actual + no declared, managed by Config Sync, prevent deletion: abandon",
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_default-name"),
				core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)),
			want: Abandon,
		},
		{
			name: "actual + no declared, system Namespace: abandon",
			actual: k8sobjects.NamespaceObject(metav1.NamespaceSystem,
				core.Annotation(metadata.ResourceIDKey, "_namespace_kube-system"),
				syncertest.ManagementEnabled),
			want: Abandon,
		},
		{
			name: "actual + no declared, gatekeeper Namespace: abandon",
			actual: k8sobjects.NamespaceObject(policycontroller.NamespaceSystem,
				core.Annotation(metadata.ResourceIDKey, "_namespace_gatekeeper-system"),
				syncertest.ManagementEnabled),
			want: Abandon,
		},
		{
			name: "actual + no declared, managed: delete",
			actual: k8sobjects.RoleObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_role_kube-system"),
				core.Name(metav1.NamespaceSystem)),
			want: Delete,
		},
		{
			name:   "actual + no declared, invalid management: abandon",
			actual: k8sobjects.RoleObject(syncertest.ManagementInvalid),
			want:   NoOp,
		},
		// Error path.
		{
			name: "no declared or actual, no op (log error)",
			want: NoOp,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scope := tc.scope
			if scope == "" {
				scope = declared.RootScope
			}
			if tc.syncName == "" {
				tc.syncName = syncName
			}

			diff := Diff{
				Declared: tc.declared,
				Actual:   tc.actual,
			}

			if d := cmp.Diff(tc.want, diff.Operation(scope, tc.syncName)); d != "" {
				t.Fatal(d)
			}
		})
	}
}

func TestThreeWay(t *testing.T) {
	tcs := []struct {
		name string
		// the git resource to which the applier syncs the state to.
		newDeclared []client.Object
		// the previously declared resources.
		previousDeclared []client.Object
		// The actual state of the resources.
		actual []client.Object
		// expected diff.
		want []Diff
	}{
		{
			name: "Update and Create - no previously declared",
			newDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/" + testNs1),
				k8sobjects.NamespaceObject("namespace/" + testNs2),
			},
			actual: []client.Object{
				k8sobjects.NamespaceObject("namespace/"+testNs1, syncertest.ManagementEnabled),
			},
			want: []Diff{
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs1),
					Actual:   k8sobjects.NamespaceObject("namespace/"+testNs1, syncertest.ManagementEnabled),
				},
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs2),
					Actual:   nil,
				},
			},
		},
		{
			name: "Update and Create - no actual",
			newDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/" + testNs1),
				k8sobjects.NamespaceObject("namespace/" + testNs2),
			},
			previousDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/"+testNs1, syncertest.ManagementEnabled),
			},
			want: []Diff{
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs1),
					Actual:   nil,
				},
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs2),
					Actual:   nil,
				},
			},
		},
		{
			name: "Update and Create - with previousDeclared and actual",
			newDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/" + testNs1),
				k8sobjects.NamespaceObject("namespace/" + testNs2),
			},
			previousDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/"+testNs1, syncertest.ManagementEnabled),
			},
			actual: []client.Object{
				k8sobjects.NamespaceObject("namespace/"+testNs2, syncertest.ManagementEnabled),
			},
			want: []Diff{
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs1),
					Actual:   nil,
				},
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs2),
					Actual:   k8sobjects.NamespaceObject("namespace/"+testNs2, syncertest.ManagementEnabled),
				},
			},
		},
		{
			name:        "Noop - with actual and no declared",
			newDeclared: []client.Object{},
			actual: []client.Object{
				k8sobjects.NamespaceObject("namespace/"+testNs1, syncertest.ManagementEnabled),
			},
			want: nil,
		},
		{
			name: "Delete - no actual",
			newDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/" + testNs1),
			},
			previousDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/"+testNs1, syncertest.ManagementEnabled),
				k8sobjects.NamespaceObject("namespace/"+testNs2, syncertest.ManagementEnabled),
			},
			want: []Diff{
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs1),
					Actual:   nil,
				},
				{
					Declared: nil,
					Actual:   k8sobjects.NamespaceObject("namespace/"+testNs2, syncertest.ManagementEnabled),
				},
			},
		},
		{
			name: "Delete - with previous declared and actual",
			newDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/" + testNs1),
			},
			previousDeclared: []client.Object{
				k8sobjects.NamespaceObject("namespace/"+testNs1, syncertest.ManagementEnabled),
				k8sobjects.NamespaceObject("namespace/"+testNs2, syncertest.ManagementEnabled),
			},
			actual: []client.Object{
				k8sobjects.NamespaceObject("namespace/" + testNs1),
				k8sobjects.NamespaceObject("namespace/" + testNs2),
			},
			want: []Diff{
				{
					Declared: k8sobjects.NamespaceObject("namespace/" + testNs1),
					Actual:   k8sobjects.NamespaceObject("namespace/" + testNs1),
				},
				{
					Declared: nil,
					Actual:   k8sobjects.NamespaceObject("namespace/"+testNs2, syncertest.ManagementEnabled),
				},
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			newDeclared := make(map[core.ID]client.Object)
			previousDeclared := make(map[core.ID]client.Object)
			actual := make(map[core.ID]client.Object)

			for _, d := range tc.newDeclared {
				newDeclared[core.IDOf(d)] = d
			}
			for _, pd := range tc.previousDeclared {
				previousDeclared[core.IDOf(pd)] = pd
			}
			for _, a := range tc.actual {
				actual[core.IDOf(a)] = a
			}

			diffs := ThreeWay(newDeclared, previousDeclared, actual)
			if diff := cmp.Diff(diffs, tc.want,
				cmpopts.SortSlices(func(x, y Diff) bool { return x.GetName() < y.GetName() })); diff != "" {
				t.Errorf(diff)
			}
		})
	}
}

func TestUnknown(t *testing.T) {
	obj := k8sobjects.NamespaceObject("hello")
	decl := map[core.ID]client.Object{
		core.IDOf(obj): obj,
	}
	actual := map[core.ID]client.Object{
		core.IDOf(obj): Unknown(),
	}
	diffs := ThreeWay(decl, nil, actual)
	if len(diffs) != 0 {
		t.Errorf("Want empty diffs with unknown; got %v", diffs)
	}
}
