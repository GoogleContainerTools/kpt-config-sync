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
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/ns.yaml",
		fake.NamespaceObject("shipping", preventDeletion))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/role.yaml", role)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare Namespace with prevent deletion lifecycle annotation")
	nt.WaitForRepoSyncs()

	err = nt.Validate("shipping", "", &corev1.Namespace{},
		nomostest.NotPendingDeletion,
		nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+2, // 2 for the test Namespace & Role
			metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("Role"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Delete the declaration and ensure the Namespace isn't deleted.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/shipping/ns.yaml")
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/shipping/role.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove Namespace shipping declaration")
	nt.WaitForRepoSyncs()

	// Ensure we kept the undeclared Namespace that had the "deletion: prevent" annotation.
	err = nt.Validate("shipping", "", &corev1.Namespace{},
		nomostest.NotPendingDeletion,
		nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		nomostest.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}
	// Ensure we deleted the undeclared Role that doesn't have the annotation.
	err = nt.ValidateNotFound("shipping-admin", "shipping", &rbacv1.Role{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount(),
			metrics.ResourceDeleted("Role"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove the lifecycle annotation from the namespace so that the namespace can be deleted after the test case.
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/ns.yaml", fake.NamespaceObject("shipping"))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the lifecycle annotation from Namespace")
	nt.WaitForRepoSyncs()
}

func TestPreventDeletionRole(t *testing.T) {
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
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/ns.yaml", fake.NamespaceObject("shipping"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/role.yaml", role)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare Role with prevent deletion lifecycle annotation")
	nt.WaitForRepoSyncs()

	// ensure that the Role is created with the preventDeletion annotation
	err = nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		nomostest.NotPendingDeletion,
		nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the declaration and ensure the Namespace isn't deleted.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/shipping/role.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove Role declaration")
	nt.WaitForRepoSyncs()

	err = nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		nomostest.NotPendingDeletion,
		nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		nomostest.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the lifecycle annotation from the role so that the role can be deleted after the test case.
	delete(role.Annotations, common.LifecycleDeleteAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/shipping/role.yaml", role)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the lifecycle annotation from Role")
	nt.WaitForRepoSyncs()

	err = nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		nomostest.NotPendingDeletion,
		nomostest.MissingAnnotation(common.LifecycleDeleteAnnotation),
		nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestPreventDeletionClusterRole(t *testing.T) {
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
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/cr.yaml", clusterRole)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare ClusterRole with prevent deletion lifecycle annotation")
	nt.WaitForRepoSyncs()

	err = nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		nomostest.NotPendingDeletion,
		nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the declaration and ensure the ClusterRole isn't deleted.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/cr.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove ClusterRole bar declaration")
	nt.WaitForRepoSyncs()

	err = nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		nomostest.NotPendingDeletion,
		nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		nomostest.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the lifecycle annotation from the cluster-role so that it can be deleted after the test case.
	delete(clusterRole.Annotations, common.LifecycleDeleteAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/cr.yaml", clusterRole)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove the lifecycle annotation from ClusterRole")
	nt.WaitForRepoSyncs()

	err = nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		nomostest.NotPendingDeletion,
		nomostest.MissingAnnotation(common.LifecycleDeleteAnnotation),
		nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
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
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/ns-%s.yaml", ns), fake.NamespaceObject(ns))
	}
	bookstoreNS := fake.NamespaceObject("bookstore")
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns-bookstore.yaml", bookstoreNS)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add special namespaces and one non-special namespace")
	nt.WaitForRepoSyncs()

	// Verify that the special namespaces have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	for ns := range specialNamespaces {
		if err := nt.Validate(ns, "", &corev1.Namespace{},
			nomostest.NotPendingDeletion,
			nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
			nomostest.HasAllNomosMetadata()); err != nil {
			nt.T.Fatal(err)
		}
	}

	// Verify that the bookstore namespace does not have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	err := nt.Validate(bookstoreNS.Name, bookstoreNS.Namespace, &corev1.Namespace{},
		nomostest.NotPendingDeletion,
		nomostest.MissingAnnotation(common.LifecycleDeleteAnnotation),
		nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Fatal(err)
	}

	for ns := range specialNamespaces {
		nt.RootRepos[configsync.RootSyncName].Remove(fmt.Sprintf("acme/ns-%s.yaml", ns))
	}
	nt.RootRepos[configsync.RootSyncName].Remove("acme/ns-bookstore.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove namespaces")
	nt.WaitForRepoSyncs()

	// Verify that the special namespaces still exist and have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	for ns := range specialNamespaces {
		if err := nt.Validate(ns, "", &corev1.Namespace{},
			nomostest.NotPendingDeletion,
			nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
			nomostest.NoConfigSyncMetadata()); err != nil {
			nt.T.Fatal(err)
		}
	}

	// Verify that the bookstore namespace is removed.
	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its  finalizer will block until all objects in that namespace are
	// deleted.
	err = nomostest.WatchForNotFound(nt, kinds.Namespace(), bookstoreNS.Name, bookstoreNS.Namespace)
	if err != nil {
		nt.T.Fatal(err)
	}
}
