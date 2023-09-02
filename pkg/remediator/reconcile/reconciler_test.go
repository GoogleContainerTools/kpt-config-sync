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

package reconcile

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/policycontroller"
	"kpt.dev/configsync/pkg/status"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/testing/testmetrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRemediator_Reconcile(t *testing.T) {
	testCases := []struct {
		name string
		// version is Version (from GVK) of the object to try to remediate.
		version string
		// declared is the state of the object as returned by the Parser.
		declared client.Object
		// actual is the current state of the object on the cluster.
		actual client.Object
		// want is the desired final state of the object on the cluster after
		// reconciliation.
		want client.Object
		// wantError is the desired error resulting from calling Reconcile, if there
		// is one.
		wantError error
	}{
		// Happy Paths.
		{
			name:     "create added object",
			version:  "v1",
			declared: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled),
			actual:   nil,
			want: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
			),
			wantError: nil,
		},
		{
			name:    "update declared object",
			version: "v1",
			declared: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
			actual: fake.ClusterRoleBindingObject(),
			want: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
				core.Label("new-label", "one"),
			),
			wantError: nil,
		},
		{
			name:     "delete removed object",
			version:  "v1",
			declared: nil,
			actual: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_clusterrolebinding_default-name")),
			want:      nil,
			wantError: nil,
		},
		// Unmanaged paths.
		{
			name:    "don't create unmanaged object",
			version: "v1",
			declared: fake.ClusterRoleBindingObject(syncertest.ManagementDisabled,
				core.Label("declared-label", "foo")),
			actual:    nil,
			want:      nil,
			wantError: nil,
		},
		{
			name:    "don't update unmanaged object",
			version: "v1",
			declared: fake.ClusterRoleBindingObject(syncertest.ManagementDisabled,
				core.Label("declared-label", "foo")),
			actual: fake.ClusterRoleBindingObject(core.Label("actual-label", "bar")),
			want: fake.ClusterRoleBindingObject(core.Label("actual-label", "bar"),
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
			),
			wantError: nil,
		},
		{
			name:     "don't delete unmanaged object",
			version:  "v1",
			declared: nil,
			actual:   fake.ClusterRoleBindingObject(),
			want: fake.ClusterRoleBindingObject(
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
			),
			wantError: nil,
		},
		{
			name:     "don't delete unmanaged object (the configsync.gke.io/resource-id annotation is incorrect)",
			version:  "v1",
			declared: nil,
			actual: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_clusterrolebinding_wrong-name")),
			want: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_clusterrolebinding_wrong-name")),
			wantError: nil,
		},
		// Bad declared management annotation paths.
		{
			name:      "don't create, and error on bad declared management annotation",
			version:   "v1",
			declared:  fake.ClusterRoleBindingObject(core.Label("declared-label", "foo"), syncertest.ManagementInvalid),
			actual:    nil,
			want:      nil,
			wantError: nonhierarchical.IllegalManagementAnnotationError(fake.Namespace("namespaces/foo"), ""),
		},
		{
			name:     "don't update, and error on bad declared management annotation",
			version:  "v1",
			declared: fake.ClusterRoleBindingObject(core.Label("declared-label", "foo"), syncertest.ManagementInvalid),
			actual:   fake.ClusterRoleBindingObject(core.Label("actual-label", "bar")),
			want: fake.ClusterRoleBindingObject(core.Label("actual-label", "bar"),
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
			),
			wantError: nonhierarchical.IllegalManagementAnnotationError(fake.Namespace("namespaces/foo"), ""),
		},
		// bad in-cluster management annotation paths.
		{
			name:    "remove bad actual management annotation",
			version: "v1",
			declared: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Label("declared-label", "foo")),
			actual: fake.ClusterRoleBindingObject(syncertest.ManagementInvalid,
				core.Label("declared-label", "foo")),
			want: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
				core.Label("declared-label", "foo")),
			wantError: nil,
		},
		{
			name:     "don't update non-Config-Sync-managed-objects with invalid management annotation",
			version:  "v1",
			declared: nil,
			actual:   fake.ClusterRoleBindingObject(core.Label("declared-label", "foo"), syncertest.ManagementInvalid),
			want: fake.ClusterRoleBindingObject(core.Label("declared-label", "foo"), syncertest.ManagementInvalid,
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
			),
			wantError: nil,
		},
		// system namespaces
		{
			name:     "don't delete kube-system Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(metav1.NamespaceSystem, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_kube-system")),
			want: fake.NamespaceObject(metav1.NamespaceSystem,
				core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
			),
		},
		{
			name:     "don't delete kube-public Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(metav1.NamespacePublic, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_kube-public")),
			want: fake.NamespaceObject(metav1.NamespacePublic,
				core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
			),
		},
		{
			name:     "don't delete default Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(metav1.NamespaceDefault, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_default")),
			want: fake.NamespaceObject(metav1.NamespaceDefault,
				core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
			),
		},
		{
			name:     "don't delete gatekeeper-system Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(policycontroller.NamespaceSystem, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_gatekeeper-system")),
			want: fake.NamespaceObject(policycontroller.NamespaceSystem,
				core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
			),
		},
		// Version difference paths.
		{
			name: "update actual object with different version",
			declared: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
			actual: fake.ClusterRoleBindingObject(),
			// Metadata change increments ResourceVersion, but not Generation
			want: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.UID("1"), core.ResourceVersion("2"), core.Generation(1),
				core.Label("new-label", "one")),
			wantError: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up the fake client that represents the initial state of the cluster.
			var existingObjs []client.Object
			if tc.actual != nil {
				existingObjs = append(existingObjs, tc.actual)
			}
			c := testingfake.NewClient(t, core.Scheme, existingObjs...)
			// Simulate the Parser having already parsed the resource and recorded it.
			d := makeDeclared(t, "unused", tc.declared)

			r := newReconciler(declared.RootReconciler, configsync.RootSyncName, c.Applier(), d, testingfake.NewFightHandler())

			// Get the triggering object for the reconcile event.
			var obj client.Object
			switch {
			case tc.declared != nil:
				obj = tc.declared
			case tc.actual != nil:
				obj = tc.actual
			default:
				t.Fatal("at least one of actual or declared must be specified for a test")
			}

			err := r.Remediate(context.Background(), core.IDOf(obj), tc.actual)
			if !errors.Is(err, tc.wantError) {
				t.Errorf("got Reconcile() = %v, want matching %v",
					err, tc.wantError)
			}

			if tc.want == nil {
				c.Check(t)
			} else {
				c.Check(t, tc.want)
			}
		})
	}
}

func TestRemediator_Reconcile_Metrics(t *testing.T) {
	testCases := []struct {
		name string
		// version is Version (from GVK) of the object to try to remediate.
		version string
		// declared is the state of the object as returned by the Parser.
		declared client.Object
		// actual is the current state of the object on the cluster.
		actual                                client.Object
		createError, updateError, deleteError status.Error
		// want is the expected final state of the object on the cluster after
		// reconciliation.
		want client.Object
		// wantError is the expected error resulting from calling Reconcile
		wantError error
		// wantMetrics is the expected metrics resulting from calling Reconcile
		wantMetrics map[*view.View][]*view.Row
	}{
		{
			name: "ConflictUpdateDoesNotExist",
			// Object declared with label
			declared: fake.RoleObject(core.Namespace("example"), core.Name("example"),
				syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
			// Object on cluster has no label and is unmanaged
			actual: fake.RoleObject(core.Namespace("example"), core.Name("example")),
			// Object update fails, because it was deleted by another client
			updateError: syncerclient.ConflictUpdateDoesNotExist(
				apierrors.NewNotFound(schema.GroupResource{Group: "rbac", Resource: "roles"}, "example"),
				fake.RoleObject(core.Namespace("example"), core.Name("example"))),
			// Object NOT updated on cluster, because update failed with conflict error
			want: fake.RoleObject(core.Namespace("example"), core.Name("example"),
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1)),
			// Expect update error returned from Remediate
			wantError: syncerclient.ConflictUpdateDoesNotExist(
				apierrors.NewNotFound(schema.GroupResource{Group: "rbac", Resource: "roles"}, "example"),
				fake.RoleObject(core.Namespace("example"), core.Name("example"))),
			// Expect resource conflict error
			wantMetrics: map[*view.View][]*view.Row{
				metrics.ResourceConflictsView: {
					{Data: &view.CountData{Value: 1}, Tags: []tag.Tag{
						// Re-enable "type" tag, if re-enabled in RecordResourceConflict
						// {Key: metrics.KeyType, Value: kinds.Role().Kind},
						{Key: metrics.KeyCommit, Value: "abc123"},
					}},
				},
			},
		},
		{
			name: "ConflictCreateAlreadyExists",
			// Object declared with label
			declared: fake.RoleObject(core.Namespace("example"), core.Name("example"),
				syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
			// Object on cluster does not exist yet
			actual: nil,
			// Object create fails, because it was already created by another client
			createError: syncerclient.ConflictCreateAlreadyExists(
				apierrors.NewNotFound(schema.GroupResource{Group: "rbac", Resource: "roles"}, "example"),
				fake.RoleObject(core.Namespace("example"), core.Name("example"))),
			// Object NOT created on cluster, because update failed with conflict error
			want: nil,
			// Expect create error returned from Remediate
			wantError: syncerclient.ConflictCreateAlreadyExists(
				apierrors.NewNotFound(schema.GroupResource{Group: "rbac", Resource: "roles"}, "example"),
				fake.RoleObject(core.Namespace("example"), core.Name("example"))),
			// Expect resource conflict error
			wantMetrics: map[*view.View][]*view.Row{
				metrics.ResourceConflictsView: {
					{Data: &view.CountData{Value: 1}, Tags: []tag.Tag{
						// Re-enable "type" tag, if re-enabled in RecordResourceConflict
						// {Key: metrics.KeyType, Value: kinds.Role().Kind},
						{Key: metrics.KeyCommit, Value: "abc123"},
					}},
				},
			},
		},
		// ConflictUpdateOldVersion will never be reported by the remediator,
		// because it uses server-side apply.
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up the fake client that represents the initial state of the cluster.
			var existingObjs []client.Object
			if tc.actual != nil {
				existingObjs = append(existingObjs, tc.actual)
			}
			fakeClient := testingfake.NewClient(t, core.Scheme, existingObjs...)
			// Simulate the Parser having already parsed the resource and recorded it.
			d := makeDeclared(t, "abc123", tc.declared)

			fakeApplier := &testingfake.Applier{Client: fakeClient}
			fakeApplier.CreateError = tc.createError
			fakeApplier.UpdateError = tc.updateError
			fakeApplier.DeleteError = tc.deleteError

			reconciler := newReconciler(declared.RootReconciler, configsync.RootSyncName, fakeApplier, d, testingfake.NewFightHandler())

			// Get the triggering object for the reconcile event.
			var obj client.Object
			switch {
			case tc.declared != nil:
				obj = tc.declared
			case tc.actual != nil:
				obj = tc.actual
			default:
				t.Fatal("at least one of actual or declared must be specified for a test")
			}

			var views []*view.View
			for view := range tc.wantMetrics {
				views = append(views, view)
			}
			m := testmetrics.RegisterMetrics(views...)

			err := reconciler.Remediate(context.Background(), core.IDOf(obj), tc.actual)
			if !errors.Is(err, tc.wantError) {
				t.Errorf("Unexpected error: want:\n%v\ngot:\n%v", tc.wantError, err)
			}

			if tc.want == nil {
				fakeClient.Check(t)
			} else {
				fakeClient.Check(t, tc.want)
			}

			for view, rows := range tc.wantMetrics {
				if diff := m.ValidateMetrics(view, rows); diff != "" {
					t.Errorf("Unexpected metrics recorded (%s): %v", view.Name, diff)
				}
			}
		})
	}
}

func makeDeclared(t *testing.T, commit string, objs ...client.Object) *declared.Resources {
	t.Helper()
	d := &declared.Resources{}
	if _, err := d.Update(context.Background(), objs, commit); err != nil {
		// Test precondition; fail early.
		t.Fatal(err)
	}
	return d
}
