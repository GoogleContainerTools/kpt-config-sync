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
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("wordpress:15.2.35")),
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
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("wordpress:15.2.35")),
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
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("wordpress:15.2.35")),
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
		v1beta1.HelmSource, helm.PrivateARHelmRegistry, remoteHelmChart.ChartName, privateSimpleHelmChartVersion, gsaARReaderEmail))
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion(remoteHelmChart.ChartName+":"+privateSimpleHelmChartVersion)),
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
		v1beta1.HelmSource, remoteHelmChart.ChartName, helm.PrivateARHelmRegistry, gsaARReaderEmail))
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
}

// TestHelmVersionRange verifies the Config Sync behavior for helm charts when helm.spec.version is specified as a range.
// Helm-sync should pull the latest helm chart version within the range.
func TestHelmVersionRange(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured)

	nt.T.Log("Create RootSync to sync from a public Helm Chart with specified version range")
	rootSyncFilePath := "../testdata/root-sync-helm-chart-version-range-cr.yaml"
	nt.T.Logf("Apply the RootSync object defined in %s", rootSyncFilePath)
	nt.MustKubectl("apply", "-f", rootSyncFilePath)
	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion("wordpress:15.4.1")),
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
	repoSyncNN := nomostest.RepoSyncNN(testNs, "rs-test")
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
		Repo:                   helm.PrivateARHelmRegistry,
		Chart:                  remoteHelmChart.ChartName,
		Auth:                   configsync.AuthGCPServiceAccount,
		GCPServiceAccountEmail: gsaARReaderEmail,
		Version:                privateNSHelmChartVersion,
		ReleaseName:            "test",
	}}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(repoSyncNN.Namespace, repoSyncNN.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to sync from a private Helm Chart without cluster scoped resources"))
	err = nt.WatchForAllSyncs(nomostest.WithRepoSha1Func(helmChartVersion(remoteHelmChart.ChartName+":"+privateNSHelmChartVersion)), nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{repoSyncNN: remoteHelmChart.ChartName}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(rs.Spec.Helm.ReleaseName+"-"+remoteHelmChart.ChartName, testNs, &appsv1.Deployment{}); err != nil {
		nt.T.Error(err)
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
		sourceRepo:    helm.PrivateARHelmRegistry,
		sourceVersion: privateCoreDNSHelmChartVersion,
		sourceChart:   privateCoreDNSHelmChart,
		sourceType:    v1beta1.HelmSource,
		gsaEmail:      gsaARReaderEmail,
		rootCommitFn:  helmChartVersion(privateCoreDNSHelmChart + ":" + privateCoreDNSHelmChartVersion),
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
		sourceRepo:    helm.PrivateARHelmRegistry,
		sourceVersion: privateCoreDNSHelmChartVersion,
		sourceChart:   privateCoreDNSHelmChart,
		sourceType:    v1beta1.HelmSource,
		gsaEmail:      gsaARReaderEmail,
		rootCommitFn:  helmChartVersion(privateCoreDNSHelmChart + ":" + privateCoreDNSHelmChartVersion),
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
		sourceRepo:    helm.PrivateARHelmRegistry,
		sourceVersion: privateCoreDNSHelmChartVersion,
		sourceChart:   privateCoreDNSHelmChart,
		sourceType:    v1beta1.HelmSource,
		gsaEmail:      gsaARReaderEmail,
		rootCommitFn:  helmChartVersion(privateCoreDNSHelmChart + ":" + privateCoreDNSHelmChartVersion),
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
		v1beta1.HelmSource, helm.PrivateARHelmRegistry, remoteHelmChart.ChartName, privateCoreDNSHelmChartVersion))
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion(remoteHelmChart.ChartName+":"+privateCoreDNSHelmChartVersion)),
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
		v1beta1.HelmSource, helm.PrivateARHelmRegistry, remoteHelmChart.ChartName, privateCoreDNSHelmChartVersion))
	err = nt.WatchForAllSyncs(nomostest.WithRootSha1Func(helmChartVersion(remoteHelmChart.ChartName+":"+privateCoreDNSHelmChartVersion)),
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
