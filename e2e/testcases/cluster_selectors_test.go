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

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	clusterregistry "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	prodClusterName         = "e2e-test-cluster"
	testClusterName         = "test-cluster-env-test"
	environmentLabelKey     = "environment"
	prodEnvironment         = "prod"
	testEnvironment         = "test"
	prodClusterSelectorName = "selector-env-prod"
	testClusterSelectorName = "selector-env-test"
	frontendNamespace       = "frontend"
	backendNamespace        = "backend"
	roleBindingName         = "bob-rolebinding"
	namespaceRepo           = "bookstore"
)

var (
	inlineProdClusterSelectorAnnotation = map[string]string{metadata.ClusterNameSelectorAnnotationKey: prodClusterName}
	legacyTestClusterSelectorAnnotation = map[string]string{metadata.LegacyClusterSelectorAnnotationKey: testClusterSelectorName}
)

func clusterObject(name, label, value string) *clusterregistry.Cluster {
	return k8sobjects.ClusterObject(core.Name(name), core.Label(label, value))
}

func clusterSelector(name, label, value string) *v1.ClusterSelector {
	cs := k8sobjects.ClusterSelectorObject(core.Name(name))
	cs.Spec.Selector.MatchLabels = map[string]string{label: value}
	return cs
}

func resourceQuota(name, namespace, pods string, annotations map[string]string) *corev1.ResourceQuota {
	rq := k8sobjects.ResourceQuotaObject(
		core.Name(name),
		core.Namespace(namespace),
		core.Annotations(annotations))
	rq.Spec.Hard = map[corev1.ResourceName]resource.Quantity{corev1.ResourcePods: resource.MustParse(pods)}
	return rq
}

func roleBinding(name, namespace string, annotations map[string]string) *rbacv1.RoleBinding {
	rb := k8sobjects.RoleBindingObject(
		core.Name(name),
		core.Namespace(namespace),
		core.Annotations(annotations))
	rb.Subjects = []rbacv1.Subject{{
		Kind: "User", Name: "bob@acme.com", APIGroup: rbacv1.GroupName,
	}}
	rb.RoleRef = rbacv1.RoleRef{
		Kind:     "ClusterRole",
		Name:     "acme-admin",
		APIGroup: rbacv1.GroupName,
	}
	return rb
}

func namespaceObject(name string, annotations map[string]string) *corev1.Namespace {
	return k8sobjects.NamespaceObject(name, core.Annotations(annotations))
}

func TestTargetingDifferentResourceQuotasToDifferentClusters(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	configMapName := clusterNameConfigMapName(nt)

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/cluster-test.yaml", testCluster))
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add test cluster and cluster registry data"))

	nt.T.Log("Add a valid cluster selector annotation to a resource quota")
	resourceQuotaName := "pod-quota"
	prodPodsQuota := "133"
	testPodsQuota := "266"
	rqInline := resourceQuota(resourceQuotaName, frontendNamespace, prodPodsQuota, inlineProdClusterSelectorAnnotation)
	rqLegacy := resourceQuota(resourceQuotaName, frontendNamespace, testPodsQuota, legacyTestClusterSelectorAnnotation)
	nsObj := namespaceObject(frontendNamespace, map[string]string{})
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", frontendNamespace), nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/frontend/quota-inline.yaml", rqInline))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/frontend/quota-legacy.yaml", rqLegacy))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a valid cluster selector annotation to a resource quota"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(resourceQuotaName, frontendNamespace, &corev1.ResourceQuota{}, resourceQuotaHasHardPods(nt, prodPodsQuota)))

	renameCluster(nt, configMapName, testClusterName)
	nt.Must(nt.WatchForAllSyncs())
	require.NoError(nt.T,
		nt.Watcher.WatchObject(kinds.ResourceQuota(), rqLegacy.Name, rqLegacy.Namespace,
			testwatcher.WatchPredicates(resourceQuotaHasHardPods(nt, testPodsQuota))))

	renameCluster(nt, configMapName, prodClusterName)
	nt.Must(nt.WatchForAllSyncs())
	require.NoError(nt.T,
		nt.Watcher.WatchObject(kinds.ResourceQuota(), rqInline.Name, rqInline.Namespace,
			testwatcher.WatchPredicates(resourceQuotaHasHardPods(nt, prodPodsQuota))))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rqInline)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))
}

func TestClusterSelectorOnObjects(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	configMapName := clusterNameConfigMapName(nt)

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, backendNamespace, inlineProdClusterSelectorAnnotation)
	nsObj := namespaceObject(backendNamespace, map[string]string{})
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace), nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a valid cluster selector annotation to a role binding"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/cluster-test.yaml", testCluster))
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add test cluster and cluster registry data"))

	nt.T.Log("Change cluster selector to match test cluster")
	rb.Annotations = legacyTestClusterSelectorAnnotation
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Change cluster selector to match test cluster"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	renameCluster(nt, configMapName, testClusterName)
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.RoleBinding(), rb.Name, rb.Namespace))

	nt.T.Log("Revert cluster selector to match prod cluster")
	rb.Annotations = inlineProdClusterSelectorAnnotation
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Revert cluster selector to match prod cluster"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	renameCluster(nt, configMapName, prodClusterName)
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.RoleBinding(), rb.Name, rb.Namespace))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))
}

func TestClusterSelectorOnNamespaces(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	configMapName := clusterNameConfigMapName(nt)

	nt.T.Log("Add a valid cluster selector annotation to a namespace")
	namespace := namespaceObject(backendNamespace, inlineProdClusterSelectorAnnotation)
	rb := roleBinding(roleBindingName, backendNamespace, inlineProdClusterSelectorAnnotation)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/namespace.yaml", namespace))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a valid cluster selector annotation to a namespace and a role binding"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(namespace.Name, namespace.Namespace, &corev1.Namespace{}))
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, namespace)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/cluster-test.yaml", testCluster))
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add test cluster and cluster registry data"))

	nt.T.Log("Change namespace to match test cluster")
	namespace.Annotations = legacyTestClusterSelectorAnnotation
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/namespace.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("Change namespace to match test cluster"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))
	nt.Must(nt.Watcher.WatchForNotFound(kinds.Namespace(), namespace.Name, namespace.Namespace))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, namespace)
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	renameCluster(nt, configMapName, testClusterName)
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(t, nt.Watcher.WatchForCurrentStatus(kinds.Namespace(), namespace.Name, namespace.Namespace))
	nt.Must(t, nt.Watcher.WatchForNotFound(kinds.RoleBinding(), rb.Name, rb.Namespace))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, namespace)
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	nt.T.Log("Updating bob-rolebinding to NOT have cluster-selector")
	rb.Annotations = map[string]string{}
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update bob-rolebinding to NOT have cluster-selector"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	nt.T.Log("Revert namespace to match prod cluster")
	namespace.Annotations = inlineProdClusterSelectorAnnotation
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/namespace.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("Revert namespace to match prod cluster"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))
	nt.Must(nt.Watcher.WatchForNotFound(kinds.Namespace(), backendNamespace, ""))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, namespace)
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))

	renameCluster(nt, configMapName, prodClusterName)
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(t, nt.Watcher.WatchForCurrentStatus(kinds.Namespace(), namespace.Name, namespace.Namespace))
	nt.Must(t, nt.Watcher.WatchForCurrentStatus(kinds.RoleBinding(), rb.Name, rb.Namespace))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, namespace)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	}))
}

func TestObjectReactsToChangeInInlineClusterSelector(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, backendNamespace, inlineProdClusterSelectorAnnotation)
	nsObj := namespaceObject(backendNamespace, map[string]string{})
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace), nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a valid cluster selector annotation to a role binding"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Modify the cluster selector to select an excluded cluster list")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: "a, b, c"}
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Modify the cluster selector to select an excluded cluster list"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))
}

func TestObjectReactsToChangeInLegacyClusterSelector(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add prod cluster, and cluster registry data")
	prodCluster := clusterObject(prodClusterName, environmentLabelKey, prodEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/cluster-prod.yaml", prodCluster))
	prodClusterSelector := clusterSelector(prodClusterSelectorName, environmentLabelKey, prodEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-prod.yaml", prodClusterSelector))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add prod cluster and cluster registry data"))

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, backendNamespace, map[string]string{metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName})
	nsObj := namespaceObject(backendNamespace, map[string]string{})
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace), nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a valid cluster selector annotation to a role binding"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Modify the cluster selector to select a different environment")
	prodClusterWithADifferentSelector := clusterSelector(prodClusterSelectorName, environmentLabelKey, "other")
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-prod.yaml", prodClusterWithADifferentSelector))
	nt.Must(rootSyncGitRepo.CommitAndPush("Modify the cluster selector to select a different environment"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))
}

func TestImporterIgnoresNonSelectedCustomResources(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/cluster-test.yaml", testCluster))
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add test cluster and cluster registry data"))

	nt.T.Log("Add CRs (not targeted to this cluster) without its CRD")
	cr := anvilCR("v1", "e2e-test-anvil", 10)
	cr.SetAnnotations(map[string]string{metadata.ClusterNameSelectorAnnotationKey: testClusterSelectorName})
	nsObj := namespaceObject(backendNamespace, map[string]string{})
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace), nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/anvil.yaml", cr))
	cr2 := anvilCR("v1", "e2e-test-anvil-2", 10)
	cr2.SetAnnotations(legacyTestClusterSelectorAnnotation)
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/anvil-2.yaml", cr2))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a custom resource without its CRD"))

	nt.Must(nt.WatchForAllSyncs())

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))
}

func TestClusterSelectorOnNamespaceRepos(t *testing.T) {
	repoSyncID := core.RepoSyncID(configsync.RepoSyncName, namespaceRepo)
	nt := nomostest.New(t,
		nomostesting.Selector,
		ntopts.SyncWithGitSource(repoSyncID),
		ntopts.RepoSyncPermissions(policy.RBACAdmin()), // NS reconciler manages rolebindings
	)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitReadWriteRepository(repoSyncID)

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, namespaceRepo, inlineProdClusterSelectorAnnotation)
	nt.Must(repoSyncGitRepo.Add("acme/bob-rolebinding.yaml", rb))
	nt.Must(repoSyncGitRepo.CommitAndPush("Add a valid cluster selector annotation to a role binding"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncKey, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	}))

	nt.T.Log("Modify the cluster selector to select an excluded cluster list")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: "a,b,,,c,d"}
	nt.Must(repoSyncGitRepo.Add("acme/bob-rolebinding.yaml", rb))
	nt.Must(repoSyncGitRepo.CommitAndPush("Modify the cluster selector to select an excluded cluster list"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	// Delete the object, since it's no longer specified for this cluster
	nt.MetricsExpectations.AddObjectDelete(configsync.RepoSyncKind, repoSyncKey, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	}))

	nt.T.Log("Switch to use ClusterSelector objects")
	clusterObj := clusterObject(prodClusterName, environmentLabelKey, prodEnvironment)
	nt.Must(repoSyncGitRepo.Add("acme/cluster.yaml", clusterObj))
	clusterSelectorObj := clusterSelector(prodClusterSelectorName, environmentLabelKey, prodEnvironment)
	nt.Must(repoSyncGitRepo.Add("acme/clusterselector.yaml", clusterSelectorObj))
	rb.Annotations = map[string]string{metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName}
	nt.Must(repoSyncGitRepo.Add("acme/bob-rolebinding.yaml", rb))
	nt.Must(repoSyncGitRepo.CommitAndPush("Add cluster registry data and use the legacy ClusterSelector"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	// Expect ClusterObject & ClusterSelector to be excluded from declared
	// resources and not applied.
	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncKey, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	}))
}

func TestInlineClusterSelectorFormat(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	configMapName := clusterNameConfigMapName(nt)
	renameCluster(nt, configMapName, "")

	nt.T.Log("Add a role binding without any cluster selectors")
	rb := roleBinding(roleBindingName, backendNamespace, map[string]string{})
	nsObj := namespaceObject(backendNamespace, map[string]string{})
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace), nsObj))
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a role binding without any cluster selectors"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Logf("Add a prod cluster selector to the role binding")
	rb.Annotations = inlineProdClusterSelectorAnnotation
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a prod cluster selector to the role binding"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	renameCluster(nt, configMapName, prodClusterName)
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(t, nt.Watcher.WatchForCurrentStatus(kinds.RoleBinding(), rb.Name, rb.Namespace))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Add an empty cluster selector annotation to a role binding")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: ""}
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add an empty cluster selector annotation to a role binding"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Add a cluster selector annotation to a role binding with a list of included clusters")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: fmt.Sprintf("a,%s,b", prodClusterName)}
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a cluster selector annotation to a role binding with a list of included clusters"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Add a cluster selector annotation to a role binding that does not include the current cluster")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: "a,,b"}
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a cluster selector annotation to a role binding with a list of excluded clusters"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.ValidateNotFound(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Add a cluster selector annotation to a role binding with a list of included clusters (with spaces)")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: fmt.Sprintf("a , %s , b", prodClusterName)}
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a cluster selector annotation to a role binding with a list of included clusters (with spaces)"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rb)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))
}

func TestClusterSelectorAnnotationConflicts(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add both cluster selector annotations to a role binding")
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{})))
	rb := roleBinding(roleBindingName, backendNamespace, map[string]string{
		metadata.ClusterNameSelectorAnnotationKey:   prodClusterName,
		metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName,
	})
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add both cluster selector annotations to a role binding"))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, selectors.ClusterSelectorAnnotationConflictErrorCode, "")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := rootSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestClusterSelectorForCRD(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Selector)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.T.Log("Add CRD without ClusterSelectors or cluster-name-selector annotation")
	crd := anvilV1CRD()
	nt.Must(rootSyncGitRepo.Add("acme/cluster/anvil-crd.yaml", crd))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a custom resource definition"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{}))

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, crd)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	// Test inline cluster-name-selector annotation
	nt.T.Log("Set the cluster-name-selector annotation to a not-selected cluster")
	crd.SetAnnotations(map[string]string{metadata.ClusterNameSelectorAnnotationKey: testClusterName})
	nt.Must(rootSyncGitRepo.Add("acme/cluster/anvil-crd.yaml", crd))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a custom resource definition with an unselected cluster-name-selector annotation"))
	nt.Must(nt.WatchForAllSyncs())
	// CRD should be marked as deleted, but may not be NotFound yet, because its
	// finalizer will block until all objects of that type are deleted.
	nt.Must(nt.Watcher.WatchForNotFound(kinds.CustomResourceDefinitionV1(), crd.Name, crd.Namespace))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, crd)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Set the cluster-name-selector annotation to a selected cluster")
	crd.SetAnnotations(map[string]string{metadata.ClusterNameSelectorAnnotationKey: prodClusterName})
	nt.Must(rootSyncGitRepo.Add("acme/cluster/anvil-crd.yaml", crd))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a custom resource definition with an selected cluster-name-selector annotation"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{}))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, crd)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	// Test legacy ClusterSelectors
	nt.T.Log("Add cluster, and cluster registry data")
	prodCluster := clusterObject(prodClusterName, environmentLabelKey, prodEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/cluster-prod.yaml", prodCluster))
	prodClusterSelector := clusterSelector(prodClusterSelectorName, environmentLabelKey, prodEnvironment)
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-prod.yaml", prodClusterSelector))
	nt.Must(rootSyncGitRepo.Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add cluster and cluster registry data"))

	nt.T.Log("Set ClusterSelector to a not-selected cluster")
	crd.SetAnnotations(legacyTestClusterSelectorAnnotation)
	nt.Must(rootSyncGitRepo.Add("acme/cluster/anvil-crd.yaml", crd))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a custom resource definition with an unselected ClusterSelector"))
	nt.Must(nt.WatchForAllSyncs())
	// CRD should be marked as deleted, but may not be NotFound yet, because its
	// finalizer will block until all objects of that type are deleted.
	nt.Must(nt.Watcher.WatchForNotFound(kinds.CustomResourceDefinitionV1(), crd.Name, crd.Namespace))

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, crd)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))

	nt.T.Log("Set ClusterSelector to a selected cluster")
	crd.SetAnnotations(map[string]string{metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName})
	nt.Must(rootSyncGitRepo.Add("acme/cluster/anvil-crd.yaml", crd))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a custom resource definition with an selected ClusterSelector"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nt.Validate(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{}))

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, crd)

	// Validate metrics.
	nt.Must(nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	}))
}

// renameCluster updates CLUSTER_NAME in the config map and restart the reconcilers.
func renameCluster(nt *nomostest.NT, configMapName, clusterName string) {
	nt.T.Logf("Change the cluster name to %q", clusterName)
	cm := &corev1.ConfigMap{}
	err := nt.KubeClient.Get(configMapName, configmanagement.ControllerNamespace, cm)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.MustMergePatch(cm, fmt.Sprintf(`{"data":{"%s":"%s"}}`, reconcilermanager.ClusterNameKey, clusterName))

	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, true)
}

// clusterNameConfigMapName returns the name of the ConfigMap that has the CLUSTER_NAME.
func clusterNameConfigMapName(nt *nomostest.NT) string {
	// The value is defined in manifests/templates/reconciler-manager.yaml
	configMapName := reconcilermanager.ManagerName

	nt.Must(nt.Validate(configMapName, configmanagement.ControllerNamespace,
		&corev1.ConfigMap{}, configMapHasClusterName(prodClusterName)))
	return configMapName
}

// configMapHasClusterName validates if the config map has the expected cluster name in `.data.CLUSTER_NAME`.
func configMapHasClusterName(clusterName string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		cm, ok := o.(*corev1.ConfigMap)
		if !ok {
			return testpredicates.WrongTypeErr(cm, &corev1.ConfigMap{})
		}
		actual := cm.Data[reconcilermanager.ClusterNameKey]
		if clusterName != actual {
			return fmt.Errorf("cluster name %q is not equal to the expected %q", actual, clusterName)
		}
		return nil
	}
}

// resourceQuotaHasHardPods validates if the resource quota has the expected hard pods in `.spec.hard.pods`.
func resourceQuotaHasHardPods(nt *nomostest.NT, pods string) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rObj, err := kinds.ToTypedObject(o, nt.KubeClient.Client.Scheme())
		if err != nil {
			return err
		}
		rq, ok := rObj.(*corev1.ResourceQuota)
		if !ok {
			return testpredicates.WrongTypeErr(rq, &corev1.ResourceQuota{})
		}
		actual := rq.Spec.Hard.Pods().String()
		if pods != actual {
			return fmt.Errorf("resource pods quota %q is not equal to the expected %q", actual, pods)
		}
		return nil
	}
}
