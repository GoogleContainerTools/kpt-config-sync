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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/helm"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
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
	privateNSHelmChartVersion      = "0.1.0"
	privateSimpleHelmChartVersion  = "1.0.0"
	privateSimpleHelmChart         = "simple"
	publicHelmRepo                 = "https://kubernetes-sigs.github.io/metrics-server"
	publicHelmChart                = "metrics-server"
	publicHelmChartVersion         = "3.8.3"
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
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("15.2.35")),
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
	if err := nt.Validate("my-wordpress", "wordpress", &appsv1.Deployment{}, containerImagePullPolicy("Always"),
		testpredicates.HasCorrectResourceRequestsLimits("wordpress",
			resource.MustParse(expectedCPURequest),
			resource.MustParse(expectedCPULimit),
			resource.MustParse(expectedMemoryRequest),
			resource.MustParse(expectedMemoryLimit)),
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
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("15.2.35")),
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
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("15.2.35")),
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
	cm0.Immutable = pointer.Bool(false)
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
	cm1.Immutable = pointer.Bool(true)
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
	cm2.Immutable = pointer.Bool(true)
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
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("15.2.35")),
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
	if err := nt.Validate("my-wordpress", "wordpress", &appsv1.Deployment{}, containerImagePullPolicy("Always"),
		testpredicates.HasCorrectResourceRequestsLimits("wordpress",
			resource.MustParse(expectedCPURequest),
			resource.MustParse(expectedCPULimit),
			resource.MustParse(expectedMemoryRequest),
			resource.MustParse(expectedMemoryLimit)),
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
	cm.Immutable = pointer.Bool(true)
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
				"valuesFile": "first"
			  },
			  {
				"name": "helm-config-map-override",
				"valuesFile": "second"
			  }
			]
		  }
		}
	  }`)

	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("15.2.35")),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "wordpress"}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// duplicated keys from later files should override the keys from previous files.
	if err := nt.Validate("my-wordpress", "wordpress", &appsv1.Deployment{}, containerImagePullPolicy("Always"),
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
		ntopts.RequireGKE(t),
	)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)

	remoteHelmChart, err := helm.PushHelmChart(nt, privateSimpleHelmChart, privateSimpleHelmChartVersion)
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "git": null, "helm": {"repo": "%s", "chart": "%s", "version": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "namespace": "", "deployNamespace": ""}}}`,
		v1beta1.HelmSource, helm.PrivateARHelmRegistry(), remoteHelmChart.ChartName, privateSimpleHelmChartVersion, gsaARReaderEmail()))
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion(privateSimpleHelmChartVersion)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: remoteHelmChart.ChartName}))
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
		ntopts.RequireGKE(t),
	)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	remoteHelmChart, err := helm.PushHelmChart(nt, privateSimpleHelmChart, privateSimpleHelmChartVersion)
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"chart": "%s", "repo": "%s", "version": "", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s", "deployNamespace": "simple"}, "git": null}}`,
		v1beta1.HelmSource, remoteHelmChart.ChartName, helm.PrivateARHelmRegistry(), gsaARReaderEmail()))
	if err = nt.Watcher.WatchObject(kinds.Deployment(), "deploy-default", "simple",
		[]testpredicates.Predicate{testpredicates.HasLabel("version", privateSimpleHelmChartVersion)}); err != nil {
		nt.T.Error(err)
	}

	// helm-sync automatically detects and updates to the new helm chart version
	newVersion := "2.5.9"
	if err := remoteHelmChart.UpdateVersion(newVersion); err != nil {
		nt.T.Fatal(err)
	}
	if err := remoteHelmChart.Push(); err != nil {
		nt.T.Fatal("failed to push helm chart update: %v", err)
	}
	if err = nt.Watcher.WatchObject(kinds.Deployment(), "deploy-default", "simple",
		[]testpredicates.Predicate{testpredicates.HasLabel("version", newVersion)}); err != nil {
		nt.T.Error(err)
	}

	newVersion = "3.0.0"
	if err := remoteHelmChart.UpdateVersion(newVersion); err != nil {
		nt.T.Fatal(err)
	}
	if err := remoteHelmChart.Push(); err != nil {
		nt.T.Fatal("failed to push helm chart update: %v", err)
	}
	if err = nt.Watcher.WatchObject(kinds.Deployment(), "deploy-default", "simple",
		[]testpredicates.Predicate{testpredicates.HasLabel("version", newVersion)}); err != nil {
		nt.T.Error(err)
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
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("15.4.1")),
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
// This test will work only with following pre-requisites:
// Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for accessing images in Artifact Registry.
func TestHelmNamespaceRepo(t *testing.T) {
	repoSyncNN := nomostest.RepoSyncNN(testNs, configsync.RepoSyncName)
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireGKE(t),
		ntopts.RepoSyncPermissions(policy.AllAdmin()), // NS reconciler manages a bunch of resources.
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name))
	nt.T.Log("Update RepoSync to sync from a public Helm Chart")
	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncNN)
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{
		Repo:    publicHelmRepo,
		Chart:   publicHelmChart,
		Auth:    configsync.AuthNone,
		Version: publicHelmChartVersion,
	}}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from a public Helm Chart with cluster-scoped type"))
	nt.WaitForRepoSyncSourceError(repoSyncNN.Namespace, repoSyncNN.Name, nonhierarchical.BadScopeErrCode, "must be Namespace-scoped type")

	remoteHelmChart, err := helm.PushHelmChart(nt, privateNSHelmChart, privateNSHelmChartVersion)
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RepoSync to sync from a private Artifact Registry")
	rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{
		Repo:                   helm.PrivateARHelmRegistry(),
		Chart:                  remoteHelmChart.ChartName,
		Auth:                   configsync.AuthGCPServiceAccount,
		GCPServiceAccountEmail: gsaARReaderEmail(),
		Version:                privateNSHelmChartVersion,
		ReleaseName:            "test",
	}}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from a private Helm Chart without cluster scoped resources"))
	err = nt.WatchForAllSyncs(nomostest.WithRepoSha1Func(helmChartVersion(privateNSHelmChartVersion)), nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{repoSyncNN: remoteHelmChart.ChartName}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(rs.Spec.Helm.ReleaseName+"-"+remoteHelmChart.ChartName, testNs, &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
	}
}

// TestHelmConfigMapNamespaceRepo verifies RepoSync can pick up values and updates from ConfigMap references.
// This test will work only with following pre-requisites:
// Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for accessing images in Artifact Registry.
func TestHelmConfigMapNamespaceRepo(t *testing.T) {
	repoSyncNN := nomostest.RepoSyncNN(testNs, configsync.RepoSyncName)
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireGKE(t),
		ntopts.RepoSyncPermissions(policy.AppsAdmin(), policy.CoreAdmin()),
		ntopts.NamespaceRepo(repoSyncNN.Namespace, repoSyncNN.Name))
	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncNN)
	cmName := "helm-cm-ns-repo"

	remoteHelmChart, err := helm.PushHelmChart(nt, privateNSHelmChart, privateNSHelmChartVersion)
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RepoSync to sync from a private Artifact Registry")
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{
		Repo:                   helm.PrivateARHelmRegistry(),
		Chart:                  remoteHelmChart.ChartName,
		Auth:                   configsync.AuthGCPServiceAccount,
		GCPServiceAccountEmail: gsaARReaderEmail(),
		Version:                privateNSHelmChartVersion,
		ReleaseName:            "test",
		ValuesFileRefs:         []v1beta1.ValuesFileRef{{Name: cmName, ValuesFile: "foo.yaml"}},
	}}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from a private Helm Chart without cluster scoped resources"))
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo\" not found")

	nt.T.Log("Create a ConfigMap that is not immutable (which should not be allowed)")
	cm0 := fake.ConfigMapObject(core.Name(cmName), core.Namespace(testNs))
	cm0.Data = map[string]string{
		"foo.yaml": `label: foo`,
	}
	cm0Copy := cm0.DeepCopy()
	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Delete(cm0); err != nil {
			nt.T.Log(err)
		}
	})
	if err := nt.KubeClient.Create(cm0); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo\" in namespace \"test-ns\" is not immutable")

	nt.T.Log("Update the ConfigMap to be immutable but to have the incorrect data key")
	cm0Copy.Immutable = pointer.Bool(true)
	cm0Copy.Data = map[string]string{
		"values.yaml": `label: foo`,
	}
	if err := nt.KubeClient.Update(cm0Copy); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo\" in namespace \"test-ns\" does not have data key \"foo.yaml\"")

	// delete the ConfigMap
	if err := nt.KubeClient.Delete(cm0Copy); err != nil {
		nt.T.Error(err)
	}
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation", "KNV1061: RepoSyncs must reference valid ConfigMaps in spec.helm.valuesFileRefs: ConfigMap \"helm-cm-ns-repo\" not found")

	nt.T.Log("Apply valid ConfigMap with values: `label: foo`")
	cm1 := fake.ConfigMapObject(core.Name(cmName), core.Namespace(testNs))
	cm1.Data = map[string]string{
		"foo.yaml": `label: foo`,
	}
	cm1.Immutable = pointer.Bool(true)
	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Delete(cm1); err != nil {
			nt.T.Log(err)
		}
	})
	if err := nt.KubeClient.Create(cm1); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.WatchForAllSyncs(nomostest.WithRepoSha1Func(helmChartVersion(privateNSHelmChartVersion)), nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{repoSyncNN: remoteHelmChart.ChartName}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(rs.Spec.Helm.ReleaseName+"-"+remoteHelmChart.ChartName, testNs, &appsv1.Deployment{},
		testpredicates.HasLabel("labelsTest", "foo")); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmARFleetWISameProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (in-project).
//
//	The test will run on a GKE cluster only with following pre-requisites
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
func TestHelmARFleetWISameProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:   true,
		crossProject:  false,
		sourceRepo:    helm.PrivateARHelmRegistry(),
		sourceVersion: privateCoreDNSHelmChartVersion,
		sourceChart:   privateCoreDNSHelmChart,
		sourceType:    v1beta1.HelmSource,
		gsaEmail:      gsaARReaderEmail(),
		rootCommitFn:  helmChartVersion(privateCoreDNSHelmChartVersion),
	})
}

// TestHelmARFleetWIDifferentProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (cross-project).
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for access image in Artifact Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=${GCP_PROJECT} \
//	   --role roles/iam.workloadIdentityUser \
//	   --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com
//
// 4. The cross-project fleet host project 'cs-dev-hub' is created.
// 5. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestHelmARFleetWIDifferentProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:   true,
		crossProject:  true,
		sourceRepo:    helm.PrivateARHelmRegistry(),
		sourceVersion: privateCoreDNSHelmChartVersion,
		sourceChart:   privateCoreDNSHelmChart,
		sourceType:    v1beta1.HelmSource,
		gsaEmail:      gsaARReaderEmail(),
		rootCommitFn:  helmChartVersion(privateCoreDNSHelmChartVersion),
	})
}

// TestHelmARGKEWorkloadIdentity tests the `gcpserviceaccount` auth type with GKE Workload Identity.
//
//	The test will run on a GKE cluster only with following pre-requisites
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
func TestHelmARGKEWorkloadIdentity(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:   false,
		crossProject:  false,
		sourceRepo:    helm.PrivateARHelmRegistry(),
		sourceVersion: privateCoreDNSHelmChartVersion,
		sourceChart:   privateCoreDNSHelmChart,
		sourceType:    v1beta1.HelmSource,
		gsaEmail:      gsaARReaderEmail(),
		rootCommitFn:  helmChartVersion(privateCoreDNSHelmChartVersion),
	})
}

// TestHelmGCENode tests the `gcenode` auth type for the Helm repository.
// The test will run on a GKE cluster only with following pre-requisites:
// 1. Workload Identity is NOT enabled
// 2. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` needs to have the following role:
//   - `roles/artifactregistry.reader` for access image in Artifact Registry.
func TestHelmGCENode(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest)

	remoteHelmChart, err := helm.PushHelmChart(nt, privateCoreDNSHelmChart, privateCoreDNSHelmChartVersion)
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"repo": "%s", "chart": "%s", "auth": "gcenode", "version": "%s", "releaseName": "my-coredns", "namespace": "coredns"}, "git": null}}`,
		v1beta1.HelmSource, helm.PrivateARHelmRegistry(), remoteHelmChart.ChartName, privateCoreDNSHelmChartVersion))
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion(privateCoreDNSHelmChartVersion)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: remoteHelmChart.ChartName}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(fmt.Sprintf("my-coredns-%s", remoteHelmChart.ChartName), "coredns", &appsv1.Deployment{},
		containerImagePullPolicy("IfNotPresent")); err != nil {
		nt.T.Error(err)
	}
}

// TestHelmARTokenAuth verifies Config Sync can pull Helm chart from private Artifact Registry with Token auth type.
// This test will work only with following pre-requisites:
// Google service account `e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for accessing images in Artifact Registry.
// A JSON key file is generated for this service account and stored in Secret Manager
func TestHelmARTokenAuth(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.SyncSource,
		ntopts.Unstructured,
		ntopts.RequireGKE(t),
	)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Fetch password from Secret Manager")
	key, err := gitproviders.FetchCloudSecret("config-sync-ci-ar-key")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Create secret for authentication")
	_, err = nt.Shell.Kubectl("create", "secret", "generic", "foo", fmt.Sprintf("--namespace=%s", v1.NSConfigManagementSystem), "--from-literal=username=_json_key", fmt.Sprintf("--from-literal=password=%s", key))
	if err != nil {
		nt.T.Fatalf("failed to create secret, err: %v", err)
	}
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "secret", "foo", "-n", v1.NSConfigManagementSystem, "--ignore-not-found")
	})

	remoteHelmChart, err := helm.PushHelmChart(nt, privateCoreDNSHelmChart, privateCoreDNSHelmChartVersion)
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "git": null, "helm": {"repo": "%s", "chart": "%s", "auth": "token", "version": "%s", "releaseName": "my-coredns", "namespace": "coredns", "secretRef": {"name" : "foo"}}}}`,
		v1beta1.HelmSource, helm.PrivateARHelmRegistry(), remoteHelmChart.ChartName, privateCoreDNSHelmChartVersion))
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion(privateCoreDNSHelmChartVersion)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: remoteHelmChart.ChartName}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(fmt.Sprintf("my-coredns-%s", remoteHelmChart.ChartName), "coredns", &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
	}
}

func helmChartVersion(chartVersion string) nomostest.Sha1Func {
	return func(*nomostest.NT, types.NamespacedName) (string, error) {
		return chartVersion, nil
	}
}
