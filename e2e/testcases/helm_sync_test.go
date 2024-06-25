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
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
)

const (
	privateCoreDNSHelmChartVersion = "1.13.3"
	privateCoreDNSHelmChart        = "coredns"
	privateNSHelmChart             = "ns-chart"
	privateSimpleHelmChartVersion  = "1.0.0"
	privateSimpleHelmChart         = "simple"
)

// TestPublicHelm can run on both Kind and GKE clusters.
// It tests Config Sync can pull from public Helm repo without any authentication.
func TestPublicHelm(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a public Helm Chart with specified release namespace and multiple inline values")
	rootSyncFilePath := "../testdata/root-sync-helm-chart-cr.yaml"
	nt.T.Logf("Apply the RootSync object defined in %s", rootSyncFilePath)
	nt.MustKubectl("apply", "-f", rootSyncFilePath)
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn("15.2.35")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "wordpress"}))
	if err != nil {
		nt.T.Fatal(err)
	}
	var expectedCPURequest string
	var expectedCPULimit string
	var expectedMemoryRequest string
	var expectedMemoryLimit string
	if nt.IsGKEAutopilot {
		expectedCPURequest = "250m"
		expectedCPULimit = expectedCPURequest
		expectedMemoryRequest = "512Mi"
		expectedMemoryLimit = expectedMemoryRequest
	} else {
		expectedCPURequest = "150m"
		expectedCPULimit = "1"
		expectedMemoryRequest = "250Mi"
		expectedMemoryLimit = "300Mi"
	}
	if err := nt.Validate("my-wordpress", "wordpress", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("wordpress", "Always"),
		testpredicates.DeploymentContainerResourcesEqual(v1beta1.ContainerResourcesSpec{
			ContainerName: "wordpress",
			CPURequest:    resource.MustParse(expectedCPURequest),
			CPULimit:      resource.MustParse(expectedCPULimit),
			MemoryRequest: resource.MustParse(expectedMemoryRequest),
			MemoryLimit:   resource.MustParse(expectedMemoryLimit),
		}),
		testpredicates.HasExactlyImage("wordpress", "bitnami/wordpress", "", "sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e"),
		testpredicates.DeploymentHasEnvVar("wordpress", "WORDPRESS_USERNAME", "test-user"),
		testpredicates.DeploymentHasEnvVar("wordpress", "WORDPRESS_EMAIL", "test-user@example.com"),
		testpredicates.DeploymentHasEnvVar("wordpress", "TEST_1", "val1"),
		testpredicates.DeploymentHasEnvVar("wordpress", "TEST_2", "val2")); err != nil {
		nt.T.Error(err)
	}
	if nt.T.Failed() {
		nt.T.FailNow()
	}

	nt.T.Log("Update RootSync to sync from a public Helm Chart with deploy namespace")
	nt.MustMergePatch(rs, `{"spec": {"helm": {"namespace": "", "deployNamespace": "deploy-ns"}}}`)
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn("15.2.35")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "wordpress"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "my-wordpress", "deploy-ns"); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForNotFound(kinds.Deployment(), "my-wordpress", "wordpress"); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Update RootSync to sync from a public Helm Chart without specified release namespace or deploy namespace")
	nt.MustMergePatch(rs, `{"spec": {"helm": {"namespace": "", "deployNamespace": ""}}}`)
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn("15.2.35")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "wordpress"}))
	if err != nil {
		nt.T.Fatal(err)
	}
	// TODO: Confirm that the change was Synced.
	// This is not currently possible using the RootSync status API, because
	// the commit didn't change, and the commit was already previously Synced.
	// If sync state could be confirmed, the objects would already be updated,
	// and we wouldn't need to wait for it.
	// if err := nt.Validate("my-wordpress", configsync.DefaultHelmReleaseNamespace, &appsv1.Deployment{}); err != nil {
	// 	nt.T.Error(err)
	// }
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), "my-wordpress", configsync.DefaultHelmReleaseNamespace); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForNotFound(kinds.Deployment(), "my-wordpress", "wordpress"); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForNotFound(kinds.Deployment(), "my-wordpress", "deploy-ns"); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmWatchConfigMap can run on both Kind and GKE clusters.
// It tests that helm-sync properly watches ConfigMaps in the RSync namespace if the RSync is created before
// the ConfigMap.
func TestHelmWatchConfigMap(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{
		"spec": {
		  "sourceFormat": "unstructured",
		  "sourceType": "helm",
		  "helm": {
			"releaseName": "my-wordpress",
			"namespace": "wordpress",
			"auth": "none",
			"repo": "https://charts.bitnami.com/bitnami",
			"chart": "wordpress",
			"version": "15.2.35",
			"values": {
			  "extraEnvVars": [
				{
				  "name": "TEST_1",
				  "value": "val1"
				},
				{
				  "name": "TEST_2",
				  "value": "val2"
				}
			  ],
			  "wordpressEmail": "test-user@example.com"
			},
			"valuesFileRefs": [
			  {
				"name": "helm-watch-config-map"
			  }
			]
		  }
		}
	  }`)

	nt.WaitForRootSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RootSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-watch-config-map\" not found")

	cmName := "helm-watch-config-map"
	nt.T.Log("Apply valid ConfigMap that is not immutable (which should not be allowed)")
	cm0 := fake.ConfigMapObject(core.Name(cmName), core.Namespace(configsync.ControllerNamespace))
	cm0.Immutable = ptr.To(false)
	cm0.Data = map[string]string{"something-else.yaml": `
image:
  digest: sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e
  pullPolicy: Never
`,
	}
	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Delete(cm0); err != nil {
			nt.T.Log(err)
		}
	})
	if err := nt.KubeClient.Create(cm0); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRootSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RootSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-watch-config-map\" in namespace \"config-management-system\" is not immutable")

	nt.T.Log("Apply the ConfigMap with values to the cluster with incorrect data key")
	cm1 := fake.ConfigMapObject(core.Name(cmName), core.Namespace(configsync.ControllerNamespace))
	cm1.Immutable = ptr.To(true)
	cm1.Data = map[string]string{"something-else.yaml": `
image:
  digest: sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e
  pullPolicy: Never
`,
	}
	if err := nt.KubeClient.Update(cm1); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRootSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RootSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-watch-config-map\" in namespace \"config-management-system\" does not have data key \"values.yaml\"")

	// delete the ConfigMap
	if err := nt.KubeClient.Delete(cm1); err != nil {
		nt.T.Error(err)
	}
	nt.WaitForRootSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RootSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-watch-config-map\" not found")

	nt.T.Log("Apply valid ConfigMap with values: imagePullPolicy: Always; wordpressUserName: test-user-1")
	cm2 := fake.ConfigMapObject(core.Name(cmName), core.Namespace(configsync.ControllerNamespace))
	cm2.Immutable = ptr.To(true)
	cm2.Data = map[string]string{"values.yaml": `
image:
  digest: sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e
  pullPolicy: Always
wordpressUsername: test-user-1
wordpressEmail: override-this@example.com
resources:
  requests:
    cpu: 150m
    memory: 250Mi
  limits:
    cpu: 1
    memory: 300Mi
mariadb:
  primary:
    persistence:
      enabled: false
service:
  type: ClusterIP`,
	}

	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Delete(cm2); err != nil {
			nt.T.Log(err)
		}
	})
	if err := nt.KubeClient.Create(cm2); err != nil {
		nt.T.Fatal(err)
	}
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn("15.2.35")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "wordpress"}))
	if err != nil {
		nt.T.Fatal(err)
	}
	var expectedCPURequest string
	var expectedCPULimit string
	var expectedMemoryRequest string
	var expectedMemoryLimit string
	if nt.IsGKEAutopilot {
		expectedCPURequest = "250m"
		expectedCPULimit = expectedCPURequest
		expectedMemoryRequest = "512Mi"
		expectedMemoryLimit = expectedMemoryRequest
	} else {
		expectedCPURequest = "150m"
		expectedCPULimit = "1"
		expectedMemoryRequest = "250Mi"
		expectedMemoryLimit = "300Mi"
	}
	if err := nt.Validate("my-wordpress", "wordpress", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("wordpress", "Always"),
		testpredicates.DeploymentContainerResourcesEqual(v1beta1.ContainerResourcesSpec{
			ContainerName: "wordpress",
			CPURequest:    resource.MustParse(expectedCPURequest),
			CPULimit:      resource.MustParse(expectedCPULimit),
			MemoryRequest: resource.MustParse(expectedMemoryRequest),
			MemoryLimit:   resource.MustParse(expectedMemoryLimit),
		}),
		testpredicates.HasExactlyImage("wordpress", "bitnami/wordpress", "", "sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e"),
		testpredicates.DeploymentHasEnvVar("wordpress", "WORDPRESS_USERNAME", "test-user-1"),
		testpredicates.DeploymentHasEnvVar("wordpress", "WORDPRESS_EMAIL", "test-user@example.com"),
		testpredicates.DeploymentHasEnvVar("wordpress", "TEST_1", "val1"),
		testpredicates.DeploymentHasEnvVar("wordpress", "TEST_2", "val2")); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmConfigMapOverride can run on both Kind and GKE clusters.
// It tests ConfigSync behavior when multiple valuesFiles are provided
func TestHelmConfigMapOverride(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured)
	cmName := "helm-config-map-override"

	cm := fake.ConfigMapObject(core.Name(cmName), core.Namespace(configsync.ControllerNamespace))
	cm.Immutable = ptr.To(true)
	cm.Data = map[string]string{
		"first": `
extraEnvVars:
- name: TEST_CM_1
  value: "cm1"
wordpressUsername: test-user-1
wordpressEmail: override-this@example.com
image:
  digest: sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e
  pullPolicy: Never`,
		"second": `
extraEnvVars:
- name: TEST_CM_2
  value: "cm2"
wordpressUsername: test-user-2
wordpressEmail: override-this@example.com
image:
  digest: sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e
  pullPolicy: Always`,
	}
	if err := nt.KubeClient.Create(cm); err != nil {
		nt.T.Fatal(err)
	}

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{
		"spec": {
		  "sourceFormat": "unstructured",
		  "sourceType": "helm",
		  "helm": {
			"releaseName": "my-wordpress",
			"namespace": "wordpress",
			"auth": "none",
			"repo": "https://charts.bitnami.com/bitnami",
			"chart": "wordpress",
			"version": "15.2.35",
			"values": {
			  "extraEnvVars": [
				{
				  "name": "TEST_INLINE",
				  "value": "inline"
				}
			  ],
			  "wordpressEmail": "test-user@example.com",
			  "service": {
				"type": "ClusterIP"
			  }
			},
			"valuesFileRefs": [
			  {
				"name": "helm-config-map-override",
				"dataKey": "first"
			  },
			  {
				"name": "helm-config-map-override",
				"dataKey": "second"
			  }
			]
		  }
		}
	  }`)

	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn("15.2.35")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "wordpress"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// duplicated keys from later files should override the keys from previous files.
	if err := nt.Validate("my-wordpress", "wordpress", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("wordpress", "Always"),
		testpredicates.HasExactlyImage("wordpress", "bitnami/wordpress", "", "sha256:362cb642db481ebf6f14eb0244fbfb17d531a84ecfe099cd3bba6810db56694e"),
		testpredicates.DeploymentHasEnvVar("wordpress", "WORDPRESS_USERNAME", "test-user-2"),
		testpredicates.DeploymentHasEnvVar("wordpress", "WORDPRESS_EMAIL", "test-user@example.com"),
		testpredicates.DeploymentHasEnvVar("wordpress", "TEST_INLINE", "inline"),
		testpredicates.DeploymentMissingEnvVar("wordpress", "TEST_CM_1"),
		testpredicates.DeploymentMissingEnvVar("wordpress", "TEST_CM_2")); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmDefaultNamespace verifies the Config Sync behavior for helm charts when neither namespace nor deployNamespace
// are set. Namespace-scoped resources with their namespace field set should be deployed to their set namespace; namespace-scoped
// resources with their namespace field not set should be deployed to the default namespace.
// This test will work only with following pre-requisites:
// Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for accessing images in Artifact Registry.
func TestHelmDefaultNamespace(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.SyncSource,
		ntopts.Unstructured,
		ntopts.RequireHelmProvider,
	)

	chart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart(privateSimpleHelmChart))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RootSync to sync from a helm chart")
	// Switch from Git to Helm
	rootSyncHelm := nt.RootSyncObjectHelm(configsync.RootSyncName, chart)
	nt.T.Log("Manually update the RepoSync object to sync from helm")
	if err := nt.KubeClient.Apply(rootSyncHelm); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: chart.Name}))
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Validate("deploy-default", "default", &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate("deploy-ns", "ns", &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
	}
}

// TestHelmLatestVersion verifies the Config Sync behavior for helm charts when helm.spec.version is not specified. The helm-sync
// binary should pull down the latest available version in this case. It also tests that if a new helm chart gets pushed, the
// chart version gets automatically updated by Config Sync.
// The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for access image in Artifact Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=${GCP_PROJECT} \
//	   --role roles/iam.workloadIdentityUser \
//	   --member "serviceAccount:${GCP_PROJECT}.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com
//
// 4. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestHelmLatestVersion(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.WorkloadIdentity,
		ntopts.Unstructured,
		ntopts.RequireHelmProvider,
	)

	newVersion := "1.0.0"
	chart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart(privateSimpleHelmChart),
		registryproviders.HelmChartVersion(newVersion))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	rs := nt.RootSyncObjectHelm(configsync.RootSyncName, chart)
	rs.Spec.Helm.Version = ""
	rs.Spec.Helm.DeployNamespace = "simple"
	rs.Spec.Helm.Period = metav1.Duration{Duration: 5 * time.Second}

	nt.T.Log("Update RootSync to sync from a helm chart")
	if err := nt.KubeClient.Apply(rs); err != nil {
		nt.T.Fatal(err)
	}
	if err = nt.Watcher.WatchObject(kinds.Deployment(), "deploy-default", "simple",
		[]testpredicates.Predicate{testpredicates.HasLabel("version", chart.Version)}); err != nil {
		nt.T.Fatal(err)
	}

	// helm-sync automatically detects and updates to the new helm chart version
	newVersion = "2.5.9"
	chart, err = nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart(privateSimpleHelmChart),
		registryproviders.HelmChartVersion(newVersion))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}
	if err = nt.Watcher.WatchObject(kinds.Deployment(), "deploy-default", "simple",
		[]testpredicates.Predicate{testpredicates.HasLabel("version", chart.Version)}); err != nil {
		nt.T.Fatal(err)
	}

	newVersion = "3.0.0"
	chart, err = nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart(privateSimpleHelmChart),
		registryproviders.HelmChartVersion(newVersion))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}
	if err = nt.Watcher.WatchObject(kinds.Deployment(), "deploy-default", "simple",
		[]testpredicates.Predicate{testpredicates.HasLabel("version", chart.Version)}); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmVersionRange verifies the Config Sync behavior for helm charts when helm.spec.version is specified as a range.
// Helm-sync should pull the latest helm chart version within the range.
func TestHelmVersionRange(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured)

	nt.T.Log("Create RootSync to sync from a public Helm Chart with specified version range")
	rootSyncFilePath := "../testdata/root-sync-helm-chart-version-range-cr.yaml"
	nt.T.Logf("Apply the RootSync object defined in %s", rootSyncFilePath)
	nt.MustKubectl("apply", "-f", rootSyncFilePath)
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn("15.4.1")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "wordpress"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.Validate("my-wordpress", "wordpress", &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
	}
}

// TestHelmNamespaceRepo verifies RepoSync does not sync the helm chart with cluster-scoped resources. It also verifies that RepoSync can successfully
// sync the namespace scoped resources, and assign the RepoSync namespace to these resources.
// Running this test on Artifact Registry has following pre-requisites:
// Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for accessing images in Artifact Registry.
func TestHelmNamespaceRepo(t *testing.T) {
	repoSyncNN := nomostest.RepoSyncNN(testNs, configsync.RepoSyncName)
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireHelmProvider,
		ntopts.RepoSyncPermissions(policy.AllAdmin()), // NS reconciler manages a bunch of resources.
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name))

	nt.T.Log("Build a Helm chart with cluster-scoped resources")
	chart, err := nt.BuildAndPushHelmPackage(repoSyncNN,
		registryproviders.HelmChartObjects(nt.Scheme, fake.NamespaceObject("foo-ns")))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RepoSync to sync from helm repo, should fail due to cluster-scope resource")
	rs := nt.RepoSyncObjectHelm(repoSyncNN, chart)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from a Helm Chart with cluster-scoped resources"))
	nt.WaitForRepoSyncSourceError(repoSyncNN.Namespace, repoSyncNN.Name, nonhierarchical.BadScopeErrCode, "must be Namespace-scoped type")

	nt.T.Log("Update the helm chart with only a namespace-scope resource")
	validChart, err := nt.BuildAndPushHelmPackage(repoSyncNN,
		registryproviders.HelmChartObjects(nt.Scheme, fake.ConfigMapObject(core.Name("foo-cm"))),
		registryproviders.HelmChartVersion("v1.1.0"))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}
	rs = nt.RepoSyncObjectHelm(repoSyncNN, validChart)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from a Helm Chart with namespace-scoped resources"))

	err = nt.WatchForAllSyncs(nomostest.WithRepoSha1Func(nomostest.HelmChartVersionShaFn(validChart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{repoSyncNN: validChart.Name}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate("foo-cm", repoSyncNN.Namespace, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmConfigMapNamespaceRepo verifies RepoSync can pick up values from
// ConfigMap references. The ConfigMap must be immutable, so updates require
// creating a new ConfigMap and changing the reference in ValuesFileRefs.
// This test will work only with following pre-requisites:
// Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for accessing images in Artifact Registry.
func TestHelmConfigMapNamespaceRepo(t *testing.T) {
	repoSyncNN := nomostest.RepoSyncNN(testNs, configsync.RepoSyncName)
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireHelmProvider,
		ntopts.RepoSyncPermissions(policy.AppsAdmin(), policy.CoreAdmin()),
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name))
	cmName := "helm-cm-ns-repo-1"

	chart, err := nt.BuildAndPushHelmPackage(repoSyncNN,
		registryproviders.HelmSourceChart(privateNSHelmChart))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RepoSync to sync from a helm chart")
	rs := nt.RepoSyncObjectHelm(repoSyncNN, chart)
	rs.Spec.Helm.ReleaseName = "test"
	rs.Spec.Helm.ValuesFileRefs = []v1beta1.ValuesFileRef{{Name: cmName, DataKey: "foo.yaml"}}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from a Helm Chart without cluster scoped resources"))
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo-1\" not found")

	nt.T.Log("Create a ConfigMap that is not immutable (which should not be allowed)")
	cm1 := fake.ConfigMapObject(core.Name(cmName), core.Namespace(testNs))
	cm1.Data = map[string]string{
		"foo.yaml": `label: foo`,
	}
	nt.T.Cleanup(func() {
		cm1 := fake.ConfigMapObject(core.Name(cmName), core.Namespace(testNs))
		if err := nt.KubeClient.Delete(cm1); err != nil {
			if !apierrors.IsNotFound(err) {
				nt.T.Log(err)
			}
		}
	})
	if err := nt.KubeClient.Create(cm1); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo-1\" in namespace \"test-ns\" is not immutable")

	nt.T.Log("Update the ConfigMap to be immutable but to have the incorrect data key")
	cm1 = fake.ConfigMapObject(core.Name(cmName), core.Namespace(testNs))
	if err := nt.KubeClient.Get(cmName, testNs, cm1); err != nil {
		nt.T.Fatal(err)
	}
	cm1.Immutable = ptr.To(true)
	cm1.Data = map[string]string{
		"values.yaml": `label: foo`,
	}
	if err := nt.KubeClient.Update(cm1); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo-1\" in namespace \"test-ns\" does not have data key \"foo.yaml\"")

	// delete the ConfigMap
	if err := nt.KubeClient.Delete(cm1); err != nil {
		nt.T.Error(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo-1\" not found")

	nt.T.Log("Create new valid ConfigMap with values: `label: foo`")
	cmName2 := "helm-cm-ns-repo-2"
	cm2 := fake.ConfigMapObject(core.Name(cmName2), core.Namespace(testNs))
	cm2.Data = map[string]string{
		"foo.yaml": `label: foo`,
	}
	cm2.Immutable = ptr.To(true)
	nt.T.Cleanup(func() {
		cm2 := fake.ConfigMapObject(core.Name(cmName2), core.Namespace(testNs))
		if err := nt.KubeClient.Delete(cm2); err != nil {
			if !apierrors.IsNotFound(err) {
				nt.T.Log(err)
			}
		}
	})
	if err := nt.KubeClient.Create(cm2); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Update ValuesFileRefs to reference new ConfigMap`")
	rs.Spec.Helm.ValuesFileRefs = []v1beta1.ValuesFileRef{{Name: cmName2, DataKey: "foo.yaml"}}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to reference new ConfigMap"))

	err = nt.WatchForAllSyncs(
		nomostest.WithRepoSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{repoSyncNN: chart.Name}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(rs.Spec.Helm.ReleaseName+"-"+chart.Name, testNs, &appsv1.Deployment{},
		testpredicates.HasLabel("labelsTest", "foo")); err != nil {
		nt.T.Fatal(err)
	}
}

// ServiceAccountFile represents a Google Service Account json file.
// https://github.com/googleapis/google-cloud-go/blob/main/auth/internal/internaldetect/filetype.go#L35
type ServiceAccountFile struct {
	Type           string `json:"type"`
	ProjectID      string `json:"project_id"`
	PrivateKeyID   string `json:"private_key_id"`
	PrivateKey     string `json:"private_key"`
	ClientEmail    string `json:"client_email"`
	ClientID       string `json:"client_id"`
	AuthURL        string `json:"auth_uri"`
	TokenURL       string `json:"token_uri"`
	UniverseDomain string `json:"universe_domain"`
}

// TestHelmARTokenAuth verifies Config Sync can pull Helm chart from private
// Artifact Registry with Token auth type.
//
// Test pre-requisites:
//   - Google service account
//     `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created
//     with `roles/artifactregistry.reader` for accessing images in Artifact
//     Registry.
//   - A JSON key file is generated for this service account and stored in
//     Secret Manager
//
// Test handles service account key rotation.
func TestHelmARTokenAuth(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.SyncSource,
		ntopts.Unstructured,
		ntopts.RequireGKE(t),
		ntopts.RequireHelmArtifactRegistry(t),
	)

	gsaKeySecretID := "config-sync-ci-ar-key"
	gsaEmail := registryproviders.ArtifactRegistryReaderEmail()
	gsaName := registryproviders.ArtifactRegistryReaderName
	gsaKeyFilePath, err := fetchServiceAccountKeyFile(nt, *e2e.GCPProject, gsaKeySecretID, gsaEmail, gsaName)
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Creating kubernetes secret for authentication")
	_, err = nt.Shell.Kubectl("create", "secret", "generic", "foo",
		"--namespace", configsync.ControllerNamespace,
		"--from-literal", "username=_json_key",
		"--from-file", fmt.Sprintf("password=%s", gsaKeyFilePath))
	if err != nil {
		nt.T.Fatalf("failed to create secret, err: %v", err)
	}
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "secret", "foo", "-n", configsync.ControllerNamespace, "--ignore-not-found")
	})

	chart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart(privateCoreDNSHelmChart))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}
	chartRepoURL, err := chart.Provider.RepositoryRemoteURL()
	if err != nil {
		nt.T.Fatalf("HelmProvider.RepositoryRemoteURL: %v", err)
	}

	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "git": null, "helm": {"repo": "%s", "chart": "%s", "auth": "token", "version": "%s", "releaseName": "my-coredns", "namespace": "coredns", "secretRef": {"name" : "foo"}}}}`,
		configsync.HelmSource, chartRepoURL, chart.Name, chart.Version))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: chart.Name}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(fmt.Sprintf("my-coredns-%s", chart.Name), "coredns", &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
	}
}

// TestHelmEmptyChart verifies Config Sync can apply an empty Helm chart.
func TestHelmEmptyChart(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.SyncSource,
		ntopts.Unstructured,
		ntopts.RequireHelmProvider,
	)

	chart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Logf("Updating RootSync to sync from the Helm chart: %s:%s", chart.Name, chart.Version)
	rs := nt.RootSyncObjectHelm(configsync.RootSyncName, chart)
	if err := nt.KubeClient.Apply(rs); err != nil {
		nt.T.Fatal(err)
	}

	// Validate that the chart syncs without error
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: chart.Name,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// Synchronize Secret Manager operations across test threads. Note that there
// can still be conflicts across periodic jobs in Prow since the Secret is shared.
var secretManagerMux sync.Mutex

// fetchServiceAccountKeyFile downloads a service account key from google
// secret manager, using the specified `gsaKeySecretID`. If the key has expired,
// a new key is generated and stored as a new version of the secret in secret
// manager. If successful, the old expired key is deleted. Then the path to the
// new key file is returned. The key file will be deleted when the current
// test ends.
func fetchServiceAccountKeyFile(nt *nomostest.NT, projectID, gsaKeySecretID, gsaEmail, gsaName string) (string, error) {
	secretManagerMux.Lock()
	defer secretManagerMux.Unlock()
	// Use a temp file to hold the secret to avoid logging the secret contents
	gsaKeyFilePath := filepath.Join(nt.TmpDir, fmt.Sprintf("%s.json", gsaName))
	nt.T.Cleanup(func() {
		nt.Must(os.RemoveAll(gsaKeyFilePath))
	})

	nt.T.Log("Listing enabled service account key versions in Secret Manager")
	out, err := nt.Shell.ExecWithDebug("gcloud", "secrets", "versions", "list",
		gsaKeySecretID,
		"--filter", "state=ENABLED",
		"--limit", "1",
		"--format", "value(name)",
		"--project", projectID)
	if err != nil {
		if !strings.Contains(string(out), "NOT_FOUND") {
			return "", err
		}
		nt.T.Log("Secret not found, bootstrapping initial Secret")
		_, err = nt.Shell.ExecWithDebug("gcloud", "secrets", "create",
			gsaKeySecretID,
			"--project", projectID)
		if err != nil {
			return "", fmt.Errorf("bootstrapping secret: %w", err)
		}
		err = generateServiceAccountKey(nt, projectID, gsaKeySecretID, gsaEmail, gsaKeyFilePath)
		if err != nil {
			return "", err
		}
		return gsaKeyFilePath, nil
	}
	// For some reason, the "name" value is just the version,
	// even tho in json the "name" field is the full name.
	// But what the access command wants is the version. So it's fine.
	gsaKeySecretVersion := strings.TrimSpace(string(out))
	if gsaKeySecretVersion == "" || strings.HasPrefix(gsaKeySecretVersion, "WARNING: ") {
		nt.T.Log("No enabled secrets versions")
		err = generateServiceAccountKey(nt, projectID, gsaKeySecretID, gsaEmail, gsaKeyFilePath)
		if err != nil {
			return "", err
		}
		return gsaKeyFilePath, nil
	}
	if _, err := strconv.Atoi(gsaKeySecretVersion); err != nil {
		return "", fmt.Errorf("converting version to int: %w", err)
	}

	nt.T.Log("Reading service account key from Secret Manager")
	_, err = nt.Shell.ExecWithDebug("gcloud", "secrets", "versions",
		"access", gsaKeySecretVersion,
		"--secret", gsaKeySecretID,
		"--out-file", gsaKeyFilePath,
		"--project", projectID)
	if err != nil {
		return "", fmt.Errorf("reading latest enabled secret version: %w", err)
	}

	nt.T.Log("Checking service account key expiration")
	gsaKeyFileBytes, err := os.ReadFile(gsaKeyFilePath)
	if err != nil {
		return "", fmt.Errorf("reading service account file: %w", err)
	}
	gsaKeyJSON := string(gsaKeyFileBytes)
	gsaKeyFile := &ServiceAccountFile{}
	if err := json.Unmarshal([]byte(gsaKeyJSON), gsaKeyFile); err != nil {
		return "", fmt.Errorf("parsing service account key file: %w", err)
	}
	gsaKeyID := gsaKeyFile.PrivateKeyID
	if gsaKeyID == "" {
		return "", errors.New("invalid service account key file: empty private_key_id")
	}
	// There's no describe for individual keys, so we have to list with a filter.
	gsaKeyName := fmt.Sprintf("projects/%s/serviceAccounts/%s/keys/%s",
		projectID, gsaEmail, gsaKeyID)
	out, err = nt.Shell.ExecWithDebug("gcloud", "iam", "service-accounts", "keys", "list",
		"--iam-account", gsaEmail,
		"--filter", fmt.Sprintf("name=%s", gsaKeyName),
		"--format", "value(validBeforeTime)",
		"--project", projectID)
	if err != nil {
		return "", fmt.Errorf("listing service account keys: %w", err)
	}
	validBeforeTimestamp := strings.TrimSpace(string(out))
	if validBeforeTimestamp == "" {
		nt.T.Log("No matching service account key found")
		err = generateServiceAccountKey(nt, projectID, gsaKeySecretID, gsaEmail, gsaKeyFilePath)
		if err != nil {
			return "", err
		}

		nt.T.Log("Destroying invalid secret version")
		_, err = nt.Shell.ExecWithDebug("gcloud", "secrets", "versions",
			"destroy", gsaKeySecretVersion,
			"--secret", gsaKeySecretID,
			"--project", projectID,
			"--quiet") // skip confirmation prompt
		if err != nil {
			return "", fmt.Errorf("destroying secret version: %w", err)
		}
		return gsaKeyFilePath, nil
	}
	validBeforeTime, err := time.Parse(time.RFC3339, validBeforeTimestamp)
	if err != nil {
		return "", fmt.Errorf("parsing service account key validBeforeTime: %w", err)
	}
	// If the key is valid for at least another hour, return its file path.
	if validBeforeTime.After(time.Now().Add(time.Hour)) {
		return gsaKeyFilePath, nil
	}
	nt.T.Log("Service account key expired")

	// Delete the invalid key file
	if err := os.RemoveAll(gsaKeyFilePath); err != nil {
		return "", fmt.Errorf("deleting service account key file: %w", err)
	}

	err = generateServiceAccountKey(nt, projectID, gsaKeySecretID, gsaEmail, gsaKeyFilePath)
	if err != nil {
		return "", err
	}

	nt.T.Log("Deleting expired service account key")
	_, err = nt.Shell.ExecWithDebug("gcloud", "iam", "service-accounts", "keys", "delete", gsaKeyID,
		"--iam-account", gsaEmail,
		"--project", projectID,
		"--quiet") // skip confirmation prompt
	if err != nil {
		return "", fmt.Errorf("deleting service account key: %w", err)
	}
	return gsaKeyFilePath, nil
}

func generateServiceAccountKey(nt *nomostest.NT, projectID, gsaKeySecretID, gsaEmail, gsaKeyFilePath string) error {
	// Recreate key if it expires in the next hour
	// Warning: This is not thread safe!
	// It's possible that this may cause flakey tests trying to refresh the key in parallel.
	// If this becomes a problem, we may need to externalize key rotation.
	nt.T.Log("Generating new service account key")
	_, err := nt.Shell.ExecWithDebug("gcloud", "iam", "service-accounts", "keys", "create", gsaKeyFilePath,
		"--iam-account", gsaEmail,
		"--project", projectID)
	if err != nil {
		return fmt.Errorf("creating service account key: %w", err)
	}
	nt.T.Log("Writing service account key to Secret Manager")
	_, err = nt.Shell.ExecWithDebug("gcloud", "secrets", "versions", "add",
		gsaKeySecretID,
		"--data-file", gsaKeyFilePath,
		"--project", projectID)
	if err != nil {
		return fmt.Errorf("adding secret version: %w", err)
	}
	return nil
}
