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

package e2e

import (
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/cli-utils/pkg/common"
)

var preventDeletion = core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)

func TestPreventDeletionNamespace(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Lifecycle)

	// Ensure the Namespace doesn't already exist.
	err := nt.ValidateNotFound("shipping", "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	role := fake.RoleObject(core.Name("shipping-admin"))
	role.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"configmaps"},
		Verbs:     []string{"get"},
	}}

	// Declare the Namespace with the lifecycle annotation, and ensure it is created.
	nsObj := fake.NamespaceObject("shipping", preventDeletion)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/ns.yaml", nsObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/role.yaml", role))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare Namespace with prevent deletion lifecycle annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate("shipping", "", &corev1.Namespace{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, role)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the declaration and ensure the Namespace isn't deleted.
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/shipping/ns.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/shipping/role.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove Namespace shipping declaration"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Ensure we kept the undeclared Namespace that had the "deletion: prevent" annotation.
	err = nt.Validate("shipping", "", &corev1.Namespace{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}
	// Ensure we deleted the undeclared Role that doesn't have the annotation.
	err = nt.ValidateNotFound("shipping-admin", "shipping", &rbacv1.Role{})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, role)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the lifecycle annotation from the namespace so that the namespace can be deleted after the test case.
	nsObj = fake.NamespaceObject("shipping")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/ns.yaml", nsObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the lifecycle annotation from Namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, role)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestPreventDeletionRole(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Lifecycle)

	// Ensure the Namespace doesn't already exist.
	err := nt.ValidateNotFound("shipping-admin", "shipping", &rbacv1.Role{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Declare the Role with the lifecycle annotation, and ensure it is created.
	role := fake.RoleObject(core.Name("shipping-admin"), preventDeletion)
	role.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"configmaps"},
		Verbs:     []string{"get"},
	}}
	nsObj := fake.NamespaceObject("shipping")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/ns.yaml", nsObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/role.yaml", role))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare Role with prevent deletion lifecycle annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// ensure that the Role is created with the preventDeletion annotation
	err = nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, role)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the declaration and ensure the Namespace isn't deleted.
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/shipping/role.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove Role declaration"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, role)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
		// Adjust operations for this edge case.
		// Deletion is prevented, but management annotations/labels are removed.
		Operations: []metrics.ObjectOperation{
			{Kind: "Role", Operation: metrics.UpdateOperation, Count: 1},
		},
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the lifecycle annotation from the role so that the role can be deleted after the test case.
	delete(role.Annotations, common.LifecycleDeleteAnnotation)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/role.yaml", role))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the lifecycle annotation from Role"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		testpredicates.NotPendingDeletion,
		testpredicates.MissingAnnotation(common.LifecycleDeleteAnnotation),
		testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, role)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestPreventDeletionClusterRole(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Lifecycle)

	// Ensure the ClusterRole doesn't already exist.
	err := nt.ValidateNotFound("test-admin", "", &rbacv1.ClusterRole{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Declare the ClusterRole with the lifecycle annotation, and ensure it is created.
	clusterRole := fake.ClusterRoleObject(core.Name("test-admin"), preventDeletion)
	clusterRole.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"configmaps"},
		Verbs:     []string{"get"},
	}}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/cr.yaml", clusterRole))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare ClusterRole with prevent deletion lifecycle annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the declaration and ensure the ClusterRole isn't deleted.
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/cr.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove ClusterRole bar declaration"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the lifecycle annotation from the cluster-role so that it can be deleted after the test case.
	delete(clusterRole.Annotations, common.LifecycleDeleteAnnotation)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/cr.yaml", clusterRole))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the lifecycle annotation from ClusterRole"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		testpredicates.NotPendingDeletion,
		testpredicates.MissingAnnotation(common.LifecycleDeleteAnnotation),
		testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, clusterRole)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func skipAutopilotManagedNamespace(nt *nomostest.NT, ns string) bool {
	managedNS, found := util.AutopilotManagedNamespaces[ns]
	return found && managedNS && nt.IsGKEAutopilot
}

func TestPreventDeletionSpecialNamespaces(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Lifecycle, ntopts.Unstructured)

	// Build list of special namespaces to test.
	// Skip namespaces managed by GKE Autopilot, if on an Autopilot cluster
	specialNamespaces := make(map[string]struct{}, len(differ.SpecialNamespaces))
	for ns := range differ.SpecialNamespaces {
		if !skipAutopilotManagedNamespace(nt, ns) {
			specialNamespaces[ns] = struct{}{}
		}
	}

	for ns := range specialNamespaces {
		checkpointProtectedNamespace(nt, ns)
		nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/ns-%s.yaml", ns), fake.NamespaceObject(ns)))
	}
	bookstoreNS := fake.NamespaceObject("bookstore")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/ns-bookstore.yaml", bookstoreNS))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add special namespaces and one non-special namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Verify that the special namespaces have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	for ns := range specialNamespaces {
		if err := nt.Validate(ns, "", &corev1.Namespace{},
			testpredicates.NotPendingDeletion,
			testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
			testpredicates.HasAllNomosMetadata()); err != nil {
			nt.T.Fatal(err)
		}
	}

	// Verify that the bookstore namespace does not have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	err := nt.Validate(bookstoreNS.Name, bookstoreNS.Namespace, &corev1.Namespace{},
		testpredicates.NotPendingDeletion,
		testpredicates.MissingAnnotation(common.LifecycleDeleteAnnotation),
		testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	for ns := range specialNamespaces {
		nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(fmt.Sprintf("acme/ns-%s.yaml", ns)))
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/ns-bookstore.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove namespaces"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Verify that the special namespaces still exist and have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	for ns := range specialNamespaces {
		if err := nt.Validate(ns, "", &corev1.Namespace{},
			testpredicates.NotPendingDeletion,
			testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
			testpredicates.NoConfigSyncMetadata()); err != nil {
			nt.T.Fatal(err)
		}
	}

	// Verify that the bookstore namespace is removed.
	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its  finalizer will block until all objects in that namespace are
	// deleted.
	err = nt.Watcher.WatchForNotFound(kinds.Namespace(), bookstoreNS.Name, bookstoreNS.Namespace)
	if err != nil {
		nt.T.Fatal(err)
	}
}
