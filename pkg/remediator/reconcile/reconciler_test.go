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
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	rbacv1beta1 "k8s.io/api/rbac/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/policycontroller"
	"kpt.dev/configsync/pkg/syncer/syncertest"
	testingfake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
	"kpt.dev/configsync/pkg/testing/fake"
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
			name:      "create added object",
			version:   "v1",
			declared:  fake.ClusterRoleBindingObject(syncertest.ManagementEnabled),
			actual:    nil,
			want:      fake.ClusterRoleBindingObject(syncertest.ManagementEnabled),
			wantError: nil,
		},
		{
			name:    "update declared object",
			version: "v1",
			declared: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
			actual: fake.ClusterRoleBindingObject(),
			want: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
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
			actual:    fake.ClusterRoleBindingObject(core.Label("actual-label", "bar")),
			want:      fake.ClusterRoleBindingObject(core.Label("actual-label", "bar")),
			wantError: nil,
		},
		{
			name:      "don't delete unmanaged object",
			version:   "v1",
			declared:  nil,
			actual:    fake.ClusterRoleBindingObject(),
			want:      fake.ClusterRoleBindingObject(),
			wantError: nil,
		},
		{
			name:     "don't delete unmanaged object (the configsync.gke.io/resource-id annotation is incorrect)",
			version:  "v1",
			declared: nil,
			actual: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "rbac.authorization.k8s.io_clusterrolebinding_wrong-name")),
			want: fake.ClusterRoleBindingObject(syncertest.ManagementEnabled,
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
			name:      "don't update, and error on bad declared management annotation",
			version:   "v1",
			declared:  fake.ClusterRoleBindingObject(core.Label("declared-label", "foo"), syncertest.ManagementInvalid),
			actual:    fake.ClusterRoleBindingObject(core.Label("actual-label", "bar")),
			want:      fake.ClusterRoleBindingObject(core.Label("actual-label", "bar")),
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
				core.Label("declared-label", "foo")),
			wantError: nil,
		},
		{
			name:      "don't update non-Config-Sync-managed-objects with invalid management annotation",
			version:   "v1",
			declared:  nil,
			actual:    fake.ClusterRoleBindingObject(core.Label("declared-label", "foo"), syncertest.ManagementInvalid),
			want:      fake.ClusterRoleBindingObject(core.Label("declared-label", "foo"), syncertest.ManagementInvalid),
			wantError: nil,
		},
		// system namespaces
		{
			name:     "don't delete kube-system Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(metav1.NamespaceSystem, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_kube-system")),
			want: fake.NamespaceObject(metav1.NamespaceSystem),
		},
		{
			name:     "don't delete kube-public Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(metav1.NamespacePublic, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_kube-public")),
			want: fake.NamespaceObject(metav1.NamespacePublic),
		},
		{
			name:     "don't delete default Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(metav1.NamespaceDefault, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_default")),
			want: fake.NamespaceObject(metav1.NamespaceDefault),
		},
		{
			name:     "don't delete gatekeeper-system Namespace",
			version:  "v1",
			declared: nil,
			actual: fake.NamespaceObject(policycontroller.NamespaceSystem, syncertest.ManagementEnabled,
				core.Annotation(metadata.ResourceIDKey, "_namespace_gatekeeper-system")),
			want: fake.NamespaceObject(policycontroller.NamespaceSystem),
		},
		// Version difference paths.
		{
			name: "update actual object with different version",
			declared: fake.ClusterRoleBindingV1Beta1Object(syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
			actual: fake.ClusterRoleBindingObject(),
			want: fake.ClusterRoleBindingV1Beta1Object(syncertest.ManagementEnabled,
				core.Label("new-label", "one")),
			wantError: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up the fake client that represents the initial state of the cluster.
			c := fakeClient(t, tc.actual)
			// Simulate the Parser having already parsed the resource and recorded it.
			d := makeDeclared(t, tc.declared)

			r := newReconciler(declared.RootReconciler, configsync.RootSyncName, c.Applier(), d)

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

func fakeClient(t *testing.T, actual ...client.Object) *testingfake.Client {
	t.Helper()
	s := runtime.NewScheme()
	err := corev1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}
	err = rbacv1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	err = rbacv1beta1.AddToScheme(s)
	if err != nil {
		t.Fatal(err)
	}

	c := testingfake.NewClient(t, s)
	for _, a := range actual {
		if a == nil {
			continue
		}
		if err := c.Create(context.Background(), a); err != nil {
			// Test precondition; fail early.
			t.Fatal(err)
		}
	}
	return c
}

func makeDeclared(t *testing.T, objs ...client.Object) *declared.Resources {
	t.Helper()
	d := &declared.Resources{}
	if _, err := d.Update(context.Background(), objs); err != nil {
		// Test precondition; fail early.
		t.Fatal(err)
	}
	return d
}
