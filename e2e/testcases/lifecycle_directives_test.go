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
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/util"
	"sigs.k8s.io/cli-utils/pkg/common"
)

var preventDeletion = core.Annotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)

func TestPreventDeletionNamespace(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Lifecycle)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Ensure the Namespace doesn't already exist.
	nt.Must(nt.ValidateNotFound("shipping", "", &corev1.Namespace{}))

	role := k8sobjects.RoleObject(core.Name("shipping-admin"))
	role.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"configmaps"},
		Verbs:     []string{"get"},
	}}

	// Declare the Namespace with the lifecycle annotation, and ensure it is created.
	nsObj := k8sobjects.NamespaceObject("shipping", preventDeletion)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/shipping/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/shipping/role.yaml", role))
	nt.Must(rootSyncGitRepo.CommitAndPush("declare Namespace with prevent deletion lifecycle annotation"))
	nt.Must(nt.WatchForAllSyncs())

	rsObj := &v1beta1.RootSync{}
	nt.Must(nt.KubeClient.Get(rootSyncNN.Name, rootSyncNN.Namespace, rsObj))
	applySetID := core.GetLabel(rsObj, metadata.ApplySetParentIDLabel)

	nt.Must(nt.Validate("shipping", "", &corev1.Namespace{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.HasAllNomosMetadata(),
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, applySetID)))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, role)

	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	// Delete the declaration and ensure the Namespace isn't deleted.
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/shipping/ns.yaml"))
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/shipping/role.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove Namespace shipping declaration"))
	nt.Must(nt.WatchForAllSyncs())

	// Ensure we kept the undeclared Namespace that had the "deletion: prevent" annotation.
	nt.Must(nt.Validate("shipping", "", &corev1.Namespace{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.NoConfigSyncMetadata(),
		testpredicates.MissingLabel(metadata.ApplySetPartOfLabel)))

	// Ensure we deleted the undeclared Role that doesn't have the annotation.
	nt.Must(nt.ValidateNotFound("shipping-admin", "shipping", &rbacv1.Role{}))

	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, role)

	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	// Remove the lifecycle annotation from the namespace so that the namespace can be deleted after the test case.
	nsObj = k8sobjects.NamespaceObject("shipping")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/shipping/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove the lifecycle annotation from Namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, role)

	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))
}

func TestPreventDeletionRole(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Lifecycle)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Ensure the Namespace doesn't already exist.
	nt.Must(nt.ValidateNotFound("shipping-admin", "shipping", &rbacv1.Role{}))

	// Declare the Role with the lifecycle annotation, and ensure it is created.
	role := k8sobjects.RoleObject(core.Name("shipping-admin"), preventDeletion)
	role.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"configmaps"},
		Verbs:     []string{"get"},
	}}
	nsObj := k8sobjects.NamespaceObject("shipping")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/shipping/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/shipping/role.yaml", role))
	nt.Must(rootSyncGitRepo.CommitAndPush("declare Role with prevent deletion lifecycle annotation"))
	nt.Must(nt.WatchForAllSyncs())

	rsObj := &v1beta1.RootSync{}
	nt.Must(nt.KubeClient.Get(rootSyncNN.Name, rootSyncNN.Namespace, rsObj))
	applySetID := core.GetLabel(rsObj, metadata.ApplySetParentIDLabel)

	// ensure that the Role is created with the preventDeletion annotation
	nt.Must(nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.HasAllNomosMetadata(),
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, applySetID)))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, role)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	// Delete the declaration and ensure the Namespace isn't deleted.
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/shipping/role.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove Role declaration"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.NoConfigSyncMetadata(),
		testpredicates.MissingLabel(metadata.ApplySetPartOfLabel)))

	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, role)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
		// Adjust operations for this edge case.
		// Deletion is prevented, but management annotations/labels are removed.
		Operations: []metrics.ObjectOperation{
			{Operation: metrics.UpdateOperation, Count: 1}, // Role
		},
	}))

	// Remove the lifecycle annotation from the role so that the role can be deleted after the test case.
	delete(role.Annotations, common.LifecycleDeleteAnnotation)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/shipping/role.yaml", role))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove the lifecycle annotation from Role"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Validate("shipping-admin", "shipping", &rbacv1.Role{},
		testpredicates.NotPendingDeletion,
		testpredicates.MissingAnnotation(common.LifecycleDeleteAnnotation),
		testpredicates.HasAllNomosMetadata(),
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, applySetID)))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, role)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))
}

func TestPreventDeletionClusterRole(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Lifecycle)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Ensure the ClusterRole doesn't already exist.
	nt.Must(nt.ValidateNotFound("test-admin", "", &rbacv1.ClusterRole{}))

	// Declare the ClusterRole with the lifecycle annotation, and ensure it is created.
	clusterRole := k8sobjects.ClusterRoleObject(core.Name("test-admin"), preventDeletion)
	clusterRole.Rules = []rbacv1.PolicyRule{{
		APIGroups: []string{""},
		Resources: []string{"configmaps"},
		Verbs:     []string{"get"},
	}}
	nt.Must(rootSyncGitRepo.Add("acme/cluster/cr.yaml", clusterRole))
	nt.Must(rootSyncGitRepo.CommitAndPush("declare ClusterRole with prevent deletion lifecycle annotation"))
	nt.Must(nt.WatchForAllSyncs())

	rsObj := &v1beta1.RootSync{}
	nt.Must(nt.KubeClient.Get(rootSyncNN.Name, rootSyncNN.Namespace, rsObj))
	applySetID := core.GetLabel(rsObj, metadata.ApplySetParentIDLabel)

	nt.Must(nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.HasAllNomosMetadata(),
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, applySetID)))

	// Delete the declaration and ensure the ClusterRole isn't deleted.
	nt.Must(rootSyncGitRepo.Remove("acme/cluster/cr.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove ClusterRole bar declaration"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		testpredicates.NotPendingDeletion,
		testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
		testpredicates.NoConfigSyncMetadata(),
		testpredicates.MissingLabel(metadata.ApplySetPartOfLabel)))

	// Remove the lifecycle annotation from the cluster-role so that it can be deleted after the test case.
	delete(clusterRole.Annotations, common.LifecycleDeleteAnnotation)
	nt.Must(rootSyncGitRepo.Add("acme/cluster/cr.yaml", clusterRole))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove the lifecycle annotation from ClusterRole"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Validate("test-admin", "", &rbacv1.ClusterRole{},
		testpredicates.NotPendingDeletion,
		testpredicates.MissingAnnotation(common.LifecycleDeleteAnnotation),
		testpredicates.HasAllNomosMetadata(),
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, applySetID)))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, clusterRole)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))
}

func skipAutopilotManagedNamespace(nt *nomostest.NT, ns string) bool {
	managedNS, found := util.AutopilotManagedNamespaces[ns]
	return found && managedNS && nt.IsGKEAutopilot
}

func TestPreventDeletionSpecialNamespaces(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.Lifecycle,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)

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
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/ns-%s.yaml", ns), k8sobjects.NamespaceObject(ns)))
	}
	bookstoreNS := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns-bookstore.yaml", bookstoreNS))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add special namespaces and one non-special namespace"))
	nt.Must(nt.WatchForAllSyncs())

	rsObj := &v1beta1.RootSync{}
	nt.Must(nt.KubeClient.Get(rootSyncID.Name, rootSyncID.Namespace, rsObj))
	applySetID := core.GetLabel(rsObj, metadata.ApplySetParentIDLabel)

	// Verify that the special namespaces have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	for ns := range specialNamespaces {
		nt.Must(nt.Validate(ns, "", &corev1.Namespace{},
			testpredicates.NotPendingDeletion,
			testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
			testpredicates.HasAllNomosMetadata(),
			testpredicates.HasLabel(metadata.ApplySetPartOfLabel, applySetID)))
	}

	// Verify that the bookstore namespace does not have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	nt.Must(nt.Validate(bookstoreNS.Name, bookstoreNS.Namespace, &corev1.Namespace{},
		testpredicates.NotPendingDeletion,
		testpredicates.MissingAnnotation(common.LifecycleDeleteAnnotation),
		testpredicates.HasAllNomosMetadata(),
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, applySetID)))

	for ns := range specialNamespaces {
		nt.Must(rootSyncGitRepo.Remove(fmt.Sprintf("acme/ns-%s.yaml", ns)))
	}
	nt.Must(rootSyncGitRepo.Remove("acme/ns-bookstore.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove namespaces"))
	nt.Must(nt.WatchForAllSyncs())

	// Verify that the special namespaces still exist and have the `client.lifecycle.config.k8s.io/deletion: detach` annotation.
	for ns := range specialNamespaces {
		nt.Must(nt.Validate(ns, "", &corev1.Namespace{},
			testpredicates.NotPendingDeletion,
			testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion),
			testpredicates.NoConfigSyncMetadata(),
			testpredicates.MissingLabel(metadata.ApplySetPartOfLabel)))
	}

	// Verify that the bookstore namespace is removed.
	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its  finalizer will block until all objects in that namespace are
	// deleted.
	nt.Must(nt.Watcher.WatchForNotFound(kinds.Namespace(), bookstoreNS.Name, bookstoreNS.Namespace))
}
