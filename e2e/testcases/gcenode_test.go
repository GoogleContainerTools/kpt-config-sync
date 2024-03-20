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
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/testing/fake"
)

const (
	syncBranch = "main"
)

// TestGCENode tests the `gcenode` auth type.
//
// The test will run on a GKE cluster only with following pre-requisites:
//
//  1. Workload Identity is NOT enabled
//  2. Access scopes for the nodes in the cluster must include `cloud-source-repos-ro`.
//  3. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` has `source.reader`
//     access to Cloud Source Repository.
//
// Public documentation:
// https://cloud.google.com/anthos-config-management/docs/how-to/installing-config-sync#git-creds-secret
func TestGCENodeCSR(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest,
		ntopts.RequireCloudSourceRepository(t),
	)

	if err := workloadidentity.ValidateDisabled(nt); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add the kustomize-components root directory to RootSync's repo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))
	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rootSync, `{
		"spec": {
			"git": {
				"dir": "kustomize-components"
			}
		}
	}`)

	err := nt.WatchForAllSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: "kustomize-components",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootReconciler), "base", "tenant-a", "tenant-b", "tenant-c")
	if err := testutils.ReconcilerPodMissingFWICredsAnnotation(nt, nomostest.DefaultRootReconcilerName); err != nil {
		nt.T.Fatal(err)
	}
}

// TestGCENodeOCI tests the `gcenode` auth type for the OCI image.
// The test will run on a GKE cluster only with following pre-requisites:
// 1. Workload Identity is NOT enabled
// 2. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` needs to have the following roles:
//   - `roles/artifactregistry.reader` for access image in Artifact Registry.
//   - `roles/containerregistry.ServiceAgent` for access image in Container Registry.
func TestGCENodeOCI(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest,
		ntopts.RequireOCIArtifactRegistry(t))

	if err := workloadidentity.ValidateDisabled(nt); err != nil {
		nt.T.Fatal(err)
	}

	tenant := "tenant-a"
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	image, err := nt.BuildAndPushOCIImage(
		nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.ImageSourcePackage("hydration/kustomize-components"),
		registryproviders.ImageVersion("v1"))
	if err != nil {
		nt.T.Fatal(err)
	}
	imageURL, err := image.RemoteAddressWithTag()
	if err != nil {
		nt.T.Fatalf("OCIImage.RemoteAddressWithTag: %v", err)
	}

	nt.T.Log("Update RootSync to sync from an OCI image in Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcenode"}, "git": null}}`,
		v1beta1.OciSource, tenant, imageURL))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByDigest(image.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: tenant,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)

	tenant = "tenant-b"
	nt.T.Log("Update RootSync to sync from an OCI image in a private Google Container Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"oci": {"image": "%s", "dir": "%s"}}}`, privateGCRImage(), tenant))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByName(privateGCRImage())),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: tenant,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
}

// TestGCENodeHelm tests the `gcenode` auth type for the Helm repository.
// The test will run on a GKE cluster only with following pre-requisites:
// 1. Workload Identity is NOT enabled
// 2. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` needs to have the following role:
//   - `roles/artifactregistry.reader` for access image in Artifact Registry.
func TestGCENodeHelm(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest,
		ntopts.RequireHelmArtifactRegistry(t))

	if err := workloadidentity.ValidateDisabled(nt); err != nil {
		nt.T.Fatal(err)
	}

	chart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart(privateCoreDNSHelmChart))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}
	chartRepoURL, err := chart.Provider.RepositoryRemoteURL()
	if err != nil {
		nt.T.Fatalf("HelmProvider.RepositoryRemoteAddress: %v", err)
	}

	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"repo": "%s", "chart": "%s", "auth": "gcenode", "version": "%s", "releaseName": "my-coredns", "namespace": "coredns"}, "git": null}}`,
		v1beta1.HelmSource, chartRepoURL, chart.Name, chart.Version))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: chart.Name}))
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(fmt.Sprintf("my-coredns-%s", chart.Name), "coredns", &appsv1.Deployment{},
		testpredicates.DeploymentContainerPullPolicyEquals("coredns", "IfNotPresent")); err != nil {
		nt.T.Error(err)
	}
}
