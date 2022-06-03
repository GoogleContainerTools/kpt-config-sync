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
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	clusterregistry "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/transform/selectors"
	"kpt.dev/configsync/pkg/importer/filesystem"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
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
	return fake.ClusterObject(core.Name(name), core.Label(label, value))
}

func clusterSelector(name, label, value string) *v1.ClusterSelector {
	cs := fake.ClusterSelectorObject(core.Name(name))
	cs.Spec.Selector.MatchLabels = map[string]string{label: value}
	return cs
}

func resourceQuota(name, pods string, annotations map[string]string) *corev1.ResourceQuota {
	rq := fake.ResourceQuotaObject(core.Name(name), core.Annotations(annotations))
	rq.Spec.Hard = map[corev1.ResourceName]resource.Quantity{corev1.ResourcePods: resource.MustParse(pods)}
	return rq
}

func roleBinding(name string, annotations map[string]string) *rbacv1.RoleBinding {
	rb := fake.RoleBindingObject(core.Name(name),
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
	return fake.NamespaceObject(name, core.Annotations(annotations))
}

func TestTargetingDifferentResourceQuotasToDifferentClusters(t *testing.T) {
	nt := nomostest.New(t)
	configMapName := clusterNameConfigMapName(nt)

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/cluster-test.yaml", testCluster)
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add test cluster and cluster registry data")

	nt.T.Log("Add a valid cluster selector annotation to a resource quota")
	resourceQuotaName := "pod-quota"
	prodPodsQuota := "133"
	testPodsQuota := "266"
	rqInline := resourceQuota(resourceQuotaName, prodPodsQuota, inlineProdClusterSelectorAnnotation)
	rqLegacy := resourceQuota(resourceQuotaName, testPodsQuota, legacyTestClusterSelectorAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", frontendNamespace),
		namespaceObject(frontendNamespace, map[string]string{}))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/quota-inline.yaml", rqInline)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/quota-legacy.yaml", rqLegacy)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a valid cluster selector annotation to a resource quota")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(resourceQuotaName, frontendNamespace, &corev1.ResourceQuota{}, resourceQuotaHasHardPods(prodPodsQuota)); err != nil {
		nt.T.Fatal(err)
	}

	renameCluster(nt, configMapName, testClusterName)
	nt.WaitForRepoSyncs()
	if err := nt.Validate(resourceQuotaName, frontendNamespace, &corev1.ResourceQuota{}, resourceQuotaHasHardPods(testPodsQuota)); err != nil {
		nt.T.Fatal(err)
	}

	renameCluster(nt, configMapName, prodClusterName)
	nt.WaitForRepoSyncs()
	if err := nt.Validate(resourceQuotaName, frontendNamespace, &corev1.ResourceQuota{}, resourceQuotaHasHardPods(prodPodsQuota)); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestClusterSelectorOnObjects(t *testing.T) {
	nt := nomostest.New(t)

	configMapName := clusterNameConfigMapName(nt)

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, inlineProdClusterSelectorAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a valid cluster selector annotation to a role binding")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/cluster-test.yaml", testCluster)
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add test cluster and cluster registry data")

	nt.T.Log("Change cluster selector to match test cluster")
	rb.Annotations = legacyTestClusterSelectorAnnotation
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Change cluster selector to match test cluster")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	renameCluster(nt, configMapName, testClusterName)
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Revert cluster selector to match prod cluster")
	rb.Annotations = inlineProdClusterSelectorAnnotation
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Revert cluster selector to match prod cluster")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	renameCluster(nt, configMapName, prodClusterName)
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestClusterSelectorOnNamespaces(t *testing.T) {
	nt := nomostest.New(t)

	configMapName := clusterNameConfigMapName(nt)

	nt.T.Log("Add a valid cluster selector annotation to a namespace")
	namespace := namespaceObject(backendNamespace, inlineProdClusterSelectorAnnotation)
	rb := roleBinding(roleBindingName, inlineProdClusterSelectorAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/namespace.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a valid cluster selector annotation to a namespace and a role binding")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(backendNamespace, "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 3,
			metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("RoleBinding"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/cluster-test.yaml", testCluster)
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add test cluster and cluster registry data")

	nt.T.Log("Change namespace to match test cluster")
	namespace.Annotations = legacyTestClusterSelectorAnnotation
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/namespace.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Change namespace to match test cluster")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}
	nomostest.WaitToTerminate(nt, kinds.Namespace(), backendNamespace, "")

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 1,
			metrics.ResourceDeleted("RoleBinding"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	renameCluster(nt, configMapName, testClusterName)
	nt.WaitForRepoSyncs()
	if err := nt.Validate(backendNamespace, "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}
	// bob-rolebinding won't reappear in the backend namespace as the cluster is inactive in the cluster-selector
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 2,
			metrics.ResourceCreated("Namespace"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Updating bob-rolebinding to NOT have cluster-selector")
	rb.Annotations = map[string]string{}
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update bob-rolebinding to NOT have cluster-selector")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 3,
			metrics.ResourceCreated("RoleBinding"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	nt.T.Log("Revert namespace to match prod cluster")
	namespace.Annotations = inlineProdClusterSelectorAnnotation
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/namespace.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Revert namespace to match prod cluster")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}
	nomostest.WaitToTerminate(nt, kinds.Namespace(), backendNamespace, "")

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 1,
			metrics.ResourceDeleted("RoleBinding"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	renameCluster(nt, configMapName, prodClusterName)
	nt.WaitForRepoSyncs()
	if err := nt.Validate(backendNamespace, "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err = nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName, 3,
			metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("RoleBinding"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestObjectReactsToChangeInInlineClusterSelector(t *testing.T) {
	nt := nomostest.New(t)

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, inlineProdClusterSelectorAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a valid cluster selector annotation to a role binding")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Modify the cluster selector to select an excluded cluster list")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: "a, b, c"}
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify the cluster selector to select an excluded cluster list")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestObjectReactsToChangeInLegacyClusterSelector(t *testing.T) {
	nt := nomostest.New(t)

	nt.T.Log("Add prod cluster, and cluster registry data")
	prodCluster := clusterObject(prodClusterName, environmentLabelKey, prodEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/cluster-prod.yaml", prodCluster)
	prodClusterSelector := clusterSelector(prodClusterSelectorName, environmentLabelKey, prodEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-prod.yaml", prodClusterSelector)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add prod cluster and cluster registry data")

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, map[string]string{metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName})
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a valid cluster selector annotation to a role binding")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Modify the cluster selector to select a different environment")
	prodClusterWithADifferentSelector := clusterSelector(prodClusterSelectorName, environmentLabelKey, "other")
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-prod.yaml", prodClusterWithADifferentSelector)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify the cluster selector to select a different environment")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestImporterIgnoresNonSelectedCustomResources(t *testing.T) {
	nt := nomostest.New(t)

	nt.T.Log("Add test cluster, and cluster registry data")
	testCluster := clusterObject(testClusterName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/cluster-test.yaml", testCluster)
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add test cluster and cluster registry data")

	nt.T.Log("Add CRs (not targeted to this cluster) without its CRD")
	cr := anvilCR("v1", "e2e-test-anvil", 10)
	cr.SetAnnotations(map[string]string{metadata.ClusterNameSelectorAnnotationKey: testClusterSelectorName})
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/anvil.yaml", cr)
	cr2 := anvilCR("v1", "e2e-test-anvil-2", 10)
	cr2.SetAnnotations(legacyTestClusterSelectorAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/anvil-2.yaml", cr2)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a custom resource without its CRD")

	nt.WaitForRepoSyncs()

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestClusterSelectorOnNamespaceRepos(t *testing.T) {
	nt := nomostest.New(t,
		ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(namespaceRepo, configsync.RepoSyncName),
	)

	nt.T.Log("Add a valid cluster selector annotation to a role binding")
	rb := roleBinding(roleBindingName, inlineProdClusterSelectorAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	nn := nomostest.RepoSyncNN(namespaceRepo, configsync.RepoSyncName)
	nt.NonRootRepos[nn].Add("acme/bob-rolebinding.yaml", rb)
	nt.NonRootRepos[nn].CommitAndPush("Add a valid cluster selector annotation to a role binding")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, namespaceRepo, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Modify the cluster selector to select an excluded cluster list")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: "a,b,,,c,d"}
	nt.NonRootRepos[nn].Add("acme/bob-rolebinding.yaml", rb)
	nt.NonRootRepos[nn].CommitAndPush("Modify the cluster selector to select an excluded cluster list")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, namespaceRepo, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Switch to use ClusterSelector objects")
	clusterObj := clusterObject(prodClusterName, environmentLabelKey, prodEnvironment)
	nt.NonRootRepos[nn].Add("acme/cluster.yaml", clusterObj)
	clusterSelectorObj := clusterSelector(prodClusterSelectorName, environmentLabelKey, prodEnvironment)
	nt.NonRootRepos[nn].Add("acme/clusterselector.yaml", clusterSelectorObj)
	rb.Annotations = map[string]string{metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName}
	nt.NonRootRepos[nn].Add("acme/bob-rolebinding.yaml", rb)
	nt.NonRootRepos[nn].CommitAndPush("Add cluster registry data and use the legacy ClusterSelector")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, namespaceRepo, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestInlineClusterSelectorFormat(t *testing.T) {
	nt := nomostest.New(t)

	configMapName := clusterNameConfigMapName(nt)
	renameCluster(nt, configMapName, "")

	nt.T.Log("Add a role binding without any cluster selectors")
	rb := roleBinding(roleBindingName, map[string]string{})
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a role binding without any cluster selectors")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Add a prod cluster selector to the role binding")
	rb.Annotations = inlineProdClusterSelectorAnnotation
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a prod cluster selector to the role binding")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	renameCluster(nt, configMapName, prodClusterName)
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add an empty cluster selector annotation to a role binding")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: ""}
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add an empty cluster selector annotation to a role binding")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add a cluster selector annotation to a role binding with a list of included clusters")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: fmt.Sprintf("a,%s,b", prodClusterName)}
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a cluster selector annotation to a role binding with a list of included clusters")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add a cluster selector annotation to a role binding with a list of excluded clusters")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: "a,,b"}
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a cluster selector annotation to a role binding with a list of excluded clusters")
	nt.WaitForRepoSyncs()
	if err := nt.ValidateNotFound(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add a cluster selector annotation to a role binding with a list of included clusters (with spaces)")
	rb.Annotations = map[string]string{metadata.ClusterNameSelectorAnnotationKey: fmt.Sprintf("a , %s , b", prodClusterName)}
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a cluster selector annotation to a role binding with a list of included clusters (with spaces)")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(roleBindingName, backendNamespace, &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestClusterSelectorAnnotationConflicts(t *testing.T) {
	nt := nomostest.New(t)

	nt.T.Log("Add both cluster selector annotations to a role binding")
	nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/eng/%s/namespace.yaml", backendNamespace),
		namespaceObject(backendNamespace, map[string]string{}))
	rb := roleBinding(roleBindingName, map[string]string{
		metadata.ClusterNameSelectorAnnotationKey:   prodClusterName,
		metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName,
	})
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/eng/backend/bob-rolebinding.yaml", rb)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add both cluster selector annotations to a role binding")
	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, selectors.ClusterSelectorAnnotationConflictErrorCode, "")
	} else {
		nt.WaitForRepoImportErrorCode(selectors.ClusterSelectorAnnotationConflictErrorCode)
	}

	err := nt.ValidateMetrics(nomostest.SyncMetricsToReconcilerSourceError(nomostest.DefaultRootReconcilerName), func() error {
		// Validate reconciler error metric is emitted.
		return nt.ValidateReconcilerErrors(nomostest.DefaultRootReconcilerName, "source")
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestClusterSelectorForCRD(t *testing.T) {
	nt := nomostest.New(t)

	nt.T.Log("Add CRD without ClusterSelectors or cluster-name-selector annotation")
	crd := anvilV1CRD()
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/anvil-crd.yaml", crd)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a custom resource definition")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		nt.T.Fatal(err)
	}

	// Test inline cluster-name-selector annotation
	nt.T.Log("Set the cluster-name-selector annotation to a not-selected cluster")
	crd.SetAnnotations(map[string]string{metadata.ClusterNameSelectorAnnotationKey: testClusterName})
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/anvil-crd.yaml", crd)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a custom resource definition with an unselected cluster-name-selector annotation")
	nt.WaitForRepoSyncs()
	_, err := nomostest.Retry(10*time.Second, func() error {
		return nt.ValidateNotFound(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{})
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set the cluster-name-selector annotation to a selected cluster")
	crd.SetAnnotations(map[string]string{metadata.ClusterNameSelectorAnnotationKey: prodClusterName})
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/anvil-crd.yaml", crd)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a custom resource definition with an selected cluster-name-selector annotation")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		nt.T.Fatal(err)
	}

	// Test legacy ClusterSelectors
	nt.T.Log("Add cluster, and cluster registry data")
	prodCluster := clusterObject(prodClusterName, environmentLabelKey, prodEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/cluster-prod.yaml", prodCluster)
	prodClusterSelector := clusterSelector(prodClusterSelectorName, environmentLabelKey, prodEnvironment)
	testClusterSelector := clusterSelector(testClusterSelectorName, environmentLabelKey, testEnvironment)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-prod.yaml", prodClusterSelector)
	nt.RootRepos[configsync.RootSyncName].Add("acme/clusterregistry/clusterselector-test.yaml", testClusterSelector)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add cluster and cluster registry data")

	nt.T.Log("Set ClusterSelector to a not-selected cluster")
	crd.SetAnnotations(legacyTestClusterSelectorAnnotation)
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/anvil-crd.yaml", crd)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a custom resource definition with an unselected ClusterSelector")
	nt.WaitForRepoSyncs()
	_, err = nomostest.Retry(10*time.Second, func() error {
		return nt.ValidateNotFound(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{})
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set ClusterSelector to a selected cluster")
	crd.SetAnnotations(map[string]string{metadata.LegacyClusterSelectorAnnotationKey: prodClusterSelectorName})
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/anvil-crd.yaml", crd)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a custom resource definition with an selected ClusterSelector")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(crd.Name, "", &apiextensionsv1.CustomResourceDefinition{}); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

// renameCluster updates CLUSTER_NAME in the config map and restart the reconcilers.
func renameCluster(nt *nomostest.NT, configMapName, clusterName string) {
	nt.T.Logf("Change the cluster name to %q", clusterName)
	cm := &corev1.ConfigMap{}
	err := nt.Get(configMapName, configmanagement.ControllerNamespace, cm)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.MustMergePatch(cm, fmt.Sprintf(`{"data":{"%s":"%s"}}`, reconcilermanager.ClusterNameKey, clusterName))

	if nt.MultiRepo {
		nomostest.DeletePodByLabel(nt, "app", reconcilermanager.ManagerName, true)
	} else {
		nomostest.DeletePodByLabel(nt, "app", filesystem.GitImporterName, false)
		nomostest.DeletePodByLabel(nt, "app", "monitor", false)
	}
}

// clusterNameConfigMapName returns the name of the ConfigMap that has the CLUSTER_NAME.
func clusterNameConfigMapName(nt *nomostest.NT) string {
	var configMapName string
	if nt.MultiRepo {
		// The value is defined in manifests/templates/reconciler-manager.yaml
		configMapName = reconcilermanager.ManagerName
	} else {
		// The value is defined in manifests/templates/git-importer.yaml
		return "cluster-name"
	}

	if err := nt.Validate(configMapName, configmanagement.ControllerNamespace,
		&corev1.ConfigMap{}, configMapHasClusterName(prodClusterName)); err != nil {
		nt.T.Fatal(err)
	}
	return configMapName
}

// configMapHasClusterName validates if the config map has the expected cluster name in `.data.CLUSTER_NAME`.
func configMapHasClusterName(clusterName string) nomostest.Predicate {
	return func(o client.Object) error {
		cm, ok := o.(*corev1.ConfigMap)
		if !ok {
			return nomostest.WrongTypeErr(cm, &corev1.ConfigMap{})
		}
		actual := cm.Data[reconcilermanager.ClusterNameKey]
		if clusterName != actual {
			return fmt.Errorf("cluster name %q is not equal to the expected %q", actual, clusterName)
		}
		return nil
	}
}

// resourceQuotaHasHardPods validates if the resource quota has the expected hard pods in `.spec.hard.pods`.
func resourceQuotaHasHardPods(pods string) nomostest.Predicate {
	return func(o client.Object) error {
		rq, ok := o.(*corev1.ResourceQuota)
		if !ok {
			return nomostest.WrongTypeErr(rq, &corev1.ResourceQuota{})
		}
		actual := rq.Spec.Hard.Pods().String()
		if pods != actual {
			return fmt.Errorf("resource pods quota %q is not equal to the expected %q", actual, pods)
		}
		return nil
	}
}
