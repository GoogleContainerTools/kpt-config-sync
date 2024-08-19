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
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
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
		ntopts.NamespaceRepo(testNs, configsync.RepoSyncName),
		ntopts.RepoSyncPermissions(policy.AllAdmin()), // NS reconciler manages a bunch of resources.
		ntopts.WithDelegatedControl)

	if err := workloadidentity.ValidateDisabled(nt); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add the kustomize-components root directory to RootSync's repo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))
	rootSync := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rootSync, `{
		"spec": {
			"git": {
				"dir": "kustomize-components"
			}
		}
	}`)

	nt.T.Log("Add the namespace-repo directory to RepoSync's repo")
	repoSync := k8sobjects.RepoSyncObjectV1Beta1(testNs, configsync.RepoSyncName)
	repoSyncRef := nomostest.RepoSyncNN(testNs, configsync.RepoSyncName)
	nt.Must(nt.NonRootRepos[repoSyncRef].Copy("../testdata/hydration/namespace-repo", "."))
	nt.Must(nt.NonRootRepos[repoSyncRef].CommitAndPush("add DRY configs to the repository"))
	nt.MustMergePatch(repoSync, `{
		"spec": {
			"git": {
				"dir": "namespace-repo"
			}
		}
	}`)

	err := nt.WatchForAllSyncs(
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: "kustomize-components",
			repoSyncRef:                             "namespace-repo",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "base", "tenant-a", "tenant-b", "tenant-c")
	if err := testutils.ReconcilerPodMissingFWICredsAnnotation(nt, nomostest.DefaultRootReconcilerName); err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateTenant(nt, repoSyncRef.Namespace, repoSyncRef.Namespace, "base")
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
		ntopts.RequireOCIArtifactRegistry(t),
		ntopts.NamespaceRepo(testNs, configsync.RepoSyncName),
		ntopts.RepoSyncPermissions(policy.AllAdmin()), // NS reconciler manages a bunch of resources.
		ntopts.WithDelegatedControl)

	if err := workloadidentity.ValidateDisabled(nt); err != nil {
		nt.T.Fatal(err)
	}

	rootSync := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	rootSyncRef := nomostest.RootSyncNN(rootSync.Name)
	rootImage, err := nt.BuildAndPushOCIImage(
		rootSyncRef,
		registryproviders.ImageSourcePackage("hydration/kustomize-components"),
		registryproviders.ImageVersion("v1"))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Update RootSync to sync from an OCI image in Artifact Registry")
	rootSyncOCI := nt.RootSyncObjectOCI(configsync.RootSyncName, rootImage)
	if err = nt.KubeClient.Apply(rootSyncOCI); err != nil {
		nt.T.Fatal(err)
	}

	repoSyncRef := nomostest.RepoSyncNN(testNs, configsync.RepoSyncName)
	nsImage, err := nt.BuildAndPushOCIImage(
		repoSyncRef,
		registryproviders.ImageSourcePackage("hydration/namespace-repo"),
		registryproviders.ImageVersion("v1"))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Update RepoSync to sync from an OCI image in Artifact Registry")
	repoSyncOCI := nt.RepoSyncObjectOCI(repoSyncRef, nsImage)
	if err = nt.KubeClient.Apply(repoSyncOCI); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByDigest(rootImage.Digest)),
		nomostest.WithRepoSha1Func(imageDigestFuncByDigest(nsImage.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: ".",
			repoSyncRef:                             ".",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "base", "tenant-a", "tenant-b", "tenant-c")
	kustomizecomponents.ValidateTenant(nt, repoSyncRef.Namespace, repoSyncRef.Namespace, "base")

	tenant := "tenant-b"
	nt.T.Log("Update RootSync to sync from an OCI image in a private Google Container Registry")
	nt.MustMergePatch(rootSync, fmt.Sprintf(`{"spec": {"oci": {"image": "%s", "dir": "%s"}}}`, privateGCRImage("kustomize-components"), tenant))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByName(privateGCRImage("kustomize-components"))),
		nomostest.WithRepoSha1Func(imageDigestFuncByDigest(nsImage.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: tenant,
			repoSyncRef:                             ".",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "../base", tenant)
}

// TestGCENodeHelm tests the `gcenode` auth type for the Helm repository.
// The test will run on a GKE cluster only with following pre-requisites:
// 1. Workload Identity is NOT enabled
// 2. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` needs to have the following role:
//   - `roles/artifactregistry.reader` for access image in Artifact Registry.
func TestGCENodeHelm(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest,
		ntopts.RequireHelmArtifactRegistry(t),
		ntopts.NamespaceRepo(testNs, configsync.RepoSyncName),
		ntopts.RepoSyncPermissions(policy.AllAdmin()), // NS reconciler manages a bunch of resources.
		ntopts.WithDelegatedControl)

	nt.Must(workloadidentity.ValidateDisabled(nt))

	rootSyncRef := nomostest.RootSyncNN(configsync.RootSyncName)
	rootChart, err := nt.BuildAndPushHelmPackage(
		rootSyncRef,
		registryproviders.HelmSourceChart(privateCoreDNSHelmChart))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Log("Update RootSync to sync from a private Artifact Registry")
	rootSyncHelm := nt.RootSyncObjectHelm(configsync.RootSyncName, rootChart.HelmChartID)
	rootSyncHelm.Spec.Helm.ReleaseName = "my-coredns"
	nt.Must(nt.KubeClient.Apply(rootSyncHelm))

	repoSyncRef := nomostest.RepoSyncNN(testNs, configsync.RepoSyncName)
	nsChart, err := nt.BuildAndPushHelmPackage(repoSyncRef,
		registryproviders.HelmSourceChart("ns-chart"))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}
	nt.T.Log("Update RepoSync to sync from a helm chart")
	repoSyncHelm := nt.RepoSyncObjectHelm(repoSyncRef, nsChart.HelmChartID)
	repoSyncHelm.Spec.Helm.ReleaseName = "my-ns-chart"
	nt.Must(nt.KubeClient.Apply(repoSyncHelm))

	nt.Must(nt.WatchForAllSyncs())

	nt.Must(nt.Validate(fmt.Sprintf("%s-%s", rootSyncHelm.Spec.Helm.ReleaseName, rootChart.Name),
		"default", &appsv1.Deployment{},
		testpredicates.IsManagedBy(nt.Scheme, declared.RootScope, rootSyncRef.Name)))
	nt.Must(nt.Validate(fmt.Sprintf("%s-%s", repoSyncHelm.Spec.Helm.ReleaseName, nsChart.Name),
		repoSyncRef.Namespace, &appsv1.Deployment{},
		testpredicates.IsManagedBy(nt.Scheme, declared.Scope(repoSyncRef.Namespace), repoSyncRef.Name)))
}
