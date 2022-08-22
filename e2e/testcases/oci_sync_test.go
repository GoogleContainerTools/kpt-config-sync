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
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// All the following images include a kustomization.yaml file in the root directory,
	// which includes resources for tenant-a, tenant-b, tenant-c, tenant-d.
	// Each tenant includes a NetworkPolicy, a Role and a RoleBinding.
	// Image structure:
	// .
	//├── base
	//│   ├── kustomization.yaml
	//│   ├── namespace.yaml
	//│   ├── networkpolicy.yaml
	//│   ├── rolebinding.yaml
	//│   └── role.yaml
	//├── kustomization.yaml
	//├── README.md
	//├── tenant-a
	//│   └── kustomization.yaml
	//├── tenant-b
	//│   └── kustomization.yaml
	//├── tenant-c
	//│   └── kustomization.yaml
	//└── tenant-d
	//    └── kustomization.yaml
	// Root kustomization.yaml content:
	// #kustomization.yaml
	// resources:
	// - tenant-a
	// - tenant-b
	// - tenant-c
	// - tenant-d
	// Base kustomization.yaml content:
	// resources:
	// - namespace.yaml
	// - rolebinding.yaml
	// - role.yaml
	// - networkpolicy.yaml
	// commonLabels:
	//  test-case: hydration

	// publicGCRImage pulls the public OCI image by the default `latest` tag
	publicGCRImage = "gcr.io/stolos-dev/config-sync-ci/kustomize-components"
	// publicARImage pulls the public OCI image by the default `latest` tag
	publicARImage = "us-docker.pkg.dev/stolos-dev/config-sync-ci-public/kustomize-components"
	// privateGCRImage pulls the private OCI image by digest
	privateGCRImage = "us.gcr.io/stolos-dev/config-sync-ci/kustomize-components@sha256:e5b67f70a0cb3e7afb539521739baece7b8918282ccd6ef6be7caa061df95a3b"
	// privateARImage pulls the private OCI image by tag
	privateARImage    = "us-docker.pkg.dev/stolos-dev/config-sync-ci-private/kustomize-components:v1"
	imageDigest       = "e5b67f70a0cb3e7afb539521739baece7b8918282ccd6ef6be7caa061df95a3b"
	gsaARReaderEmail  = "e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com"
	gsaGCRReaderEmail = "e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com"
)

// TestPublicOCI can run on both Kind and GKE clusters.
// It tests Config Sync can pull from public OCI images without any authentication.
func TestPublicOCI(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured)
	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a public OCI image in AR")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"image": "%s", "auth": "none"}, "git": null}}`,
		v1beta1.OciSource, publicARImage))
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
			v1beta1.GitSource, origRepoURL))
	})
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(fixedOCIDigest(imageDigest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "."}))
	validateAllTenants(nt, string(declared.RootReconciler), "base", "tenant-a", "tenant-b", "tenant-c", "tenant-d")

	tenant := "tenant-a"
	nt.T.Logf("Update RootSync to sync %s from a public OCI image in GCR", tenant)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"oci": {"image": "%s", "dir": "%s"}}}`, publicGCRImage, tenant))
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(fixedOCIDigest(imageDigest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "tenant-a"}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
}

// TestOCIGCENode tests the `gcenode` auth type for the OCI image.
// The test will run on a GKE cluster only with following pre-requisites:
// 1. Workload Identity is NOT enabled
// 2. The Compute Engine default service account `PROJECT_ID-compute@developer.gserviceaccount.com` needs to have the following roles:
//   - `roles/artifactregistry.reader` for access image in Artifact Registry.
//   - `roles/containerregistry.ServiceAgent` for access image in Container Registry.
func TestGCENodeOCI(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest)

	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	tenant := "tenant-a"

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from an OCI image in Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcenode"}, "git": null}}`,
		v1beta1.OciSource, tenant, privateARImage))
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
			v1beta1.GitSource, origRepoURL))
	})
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(fixedOCIDigest(imageDigest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)

	tenant = "tenant-b"
	nt.T.Log("Update RootSync to sync from an OCI image in a private Google Container Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"oci": {"image": "%s", "dir": "%s"}}}`, privateGCRImage, tenant))
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(fixedOCIDigest(imageDigest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
}

// TestOCIARGKEWorkloadIdentity tests the `gcpserviceaccount` auth type with GKE Workload Identity.
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for access image in Artifact Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//	   --role roles/iam.workloadIdentityUser \
//	   --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com
//
// 4. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestOCIARGKEWorkloadIdentity(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  false,
		crossProject: false,
		sourceRepo:   privateARImage,
		sourceType:   v1beta1.OciSource,
		gsaEmail:     gsaARReaderEmail,
		rootCommitFn: fixedOCIDigest(imageDigest),
	})
}

// TestOCIGCRGKEWorkloadIdentity tests the `gcpserviceaccount` auth type with GKE Workload Identity.
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/containerregistry.ServiceAgent` for access image in Container Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//	   --role roles/iam.workloadIdentityUser \
//	   --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com
//
// 4. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestOCIGCRGKEWorkloadIdentity(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  false,
		crossProject: false,
		sourceRepo:   privateGCRImage,
		sourceType:   v1beta1.OciSource,
		gsaEmail:     gsaGCRReaderEmail,
		rootCommitFn: fixedOCIDigest(imageDigest),
	})
}

// TestOCIARFleetWISameProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (in-project).
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for access image in Artifact Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//	   --role roles/iam.workloadIdentityUser \
//	   --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com
//
// 4. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestOCIARFleetWISameProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  true,
		crossProject: false,
		sourceRepo:   privateARImage,
		sourceType:   v1beta1.OciSource,
		gsaEmail:     gsaARReaderEmail,
		rootCommitFn: fixedOCIDigest(imageDigest),
	})
}

// TestOCIGCRFleetWISameProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (in-project).
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/containerregistry.ServiceAgent` for access image in Container Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//	   --role roles/iam.workloadIdentityUser \
//	   --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com
//
// 4. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestOCIGCRFleetWISameProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  true,
		crossProject: false,
		sourceRepo:   privateGCRImage,
		sourceType:   v1beta1.OciSource,
		gsaEmail:     gsaGCRReaderEmail,
		rootCommitFn: fixedOCIDigest(imageDigest),
	})
}

// TestOCIARFleetWIDifferentProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (cross-project).
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for access image in Artifact Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//	   --role roles/iam.workloadIdentityUser \
//	   --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com
//
// 4. The cross-project fleet host project 'cs-dev-hub' is created.
// 5. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestOCIARFleetWIDifferentProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  true,
		crossProject: true,
		sourceRepo:   privateARImage,
		sourceType:   v1beta1.OciSource,
		gsaEmail:     gsaARReaderEmail,
		rootCommitFn: fixedOCIDigest(imageDigest),
	})
}

// TestOCIGCRFleetWIDifferentProject tests the `gcpserviceaccount` auth type with Fleet Workload Identity (cross-project).
//
//	The test will run on a GKE cluster only with following pre-requisites
//
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/containerregistry.ServiceAgent` for access image in Container Registry.
// 3. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//
//	gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//	   --role roles/iam.workloadIdentityUser \
//	   --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//	   e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com
//
// 4. The cross-project fleet host project 'cs-dev-hub' is created.
// 5. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestOCIGCRFleetWIDifferentProject(t *testing.T) {
	testWorkloadIdentity(t, workloadIdentityTestSpec{
		fleetWITest:  true,
		crossProject: true,
		sourceRepo:   privateGCRImage,
		sourceType:   v1beta1.OciSource,
		gsaEmail:     gsaGCRReaderEmail,
		rootCommitFn: fixedOCIDigest(imageDigest),
	})
}

func TestSwitchFromGitToOci(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured)
	namespace := "bookinfo"
	managerScope := string(declared.RootReconciler)
	// file path to the local RepoSync YAML file which syncs from a public Git repo that contains a service account object.
	rsGitYAMLFile := "../testdata/reconciler-manager/reposync-sample.yaml"
	// file path to the local RepoSync YAML file which syncs from a public OCI image that contains a role.
	rsOCIYAMLFile := "../testdata/reconciler-manager/reposync-oci.yaml"
	// file path to the RepoSync config in the root repository.
	repoResourcePath := "acme/reposync-bookinfo.yaml"
	// The Sha1Func that returns the fixed OCI digest of the namespace repo package.
	nsRepoOCIDigestShaFunc := fixedOCIDigest("e118253937b078ea35032c665855f9c51ba23715302445bea663cae61a2a34f9")

	implictNs := &corev1.Namespace{}
	implictNs.Name = namespace

	nt.T.Cleanup(func() {
		// Stop the Config Sync webhook to allow manual deletion of the managed implicit namespace
		nomostest.StopWebhook(nt)
		if err := nt.Delete(implictNs); err != nil {
			nt.T.Error(err)
		}
		nomostest.WaitToTerminateObject(nt, implictNs)
	})

	// Verify the central controlled configuration: switch from Git to OCI
	// Backward compatibility check. Previously managed RepoSync objects without sourceType should still work.
	nt.T.Log("Add the RepoSync object to the Root Repo")
	nt.RootRepos[configsync.RootSyncName].Copy(rsGitYAMLFile, repoResourcePath)
	nt.RootRepos[configsync.RootSyncName].Add("acme/cluster/cr.yaml", nomostest.RepoSyncClusterRole())
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("configure RepoSync in the root repository")
	// nt.WaitForRepoSyncs only waits for the root repo being synced because the reposync is not tracked by nt.
	nt.WaitForRepoSyncs()
	nt.T.Log("Verify an implicit namespace is created")
	if err := nt.Validate(namespace, "", implictNs,
		nomostest.HasAnnotation(metadata.ResourceManagerKey, managerScope),
		nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate(configsync.RepoSyncName, namespace, &v1beta1.RepoSync{}, isSourceType(v1beta1.GitSource)); err != nil {
		nt.T.Error(err)
	}
	nt.T.Log("Verify the namespace objects are synced")
	nt.WaitForSync(kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, namespace,
		nt.DefaultWaitTimeout, nomostest.RemoteNsRepoSha1Fn, nomostest.RepoSyncHasStatusSyncCommit, nil)
	if err := nt.Validate("bookinfo-sa", namespace, &corev1.ServiceAccount{},
		nomostest.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Error(err)
	}
	// Switch from Git to OCI
	nt.T.Log("Update the RepoSync object to sync from OCI")
	nt.RootRepos[configsync.RootSyncName].Copy(rsOCIYAMLFile, repoResourcePath)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("configure RepoSync to sync from OCI in the root repository")
	nt.WaitForRepoSyncs()
	if err := nt.Validate(configsync.RepoSyncName, namespace, &v1beta1.RepoSync{}, isSourceType(v1beta1.OciSource)); err != nil {
		nt.T.Error(err)
	}
	nt.T.Log("Verify the namespace objects are updated")
	nt.WaitForSync(kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, namespace,
		nt.DefaultWaitTimeout, nsRepoOCIDigestShaFunc, nomostest.RepoSyncHasStatusSyncCommit, nil)
	if err := nt.Validate("bookinfo-admin", namespace, &rbacv1.Role{},
		nomostest.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.ValidateNotFound("bookinfo-sa", namespace, &corev1.ServiceAccount{}); err != nil {
		nt.T.Error(err)
	}

	// Verify the manual configuration: switch from Git to OCI
	nt.T.Log("Remove RepoSync from the root repository")
	nt.RootRepos[configsync.RootSyncName].Remove(repoResourcePath)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove RepoSync from the root repository")
	nt.WaitForRepoSyncs()
	nt.T.Log("Verify the RepoSync object doesn't exist")
	if err := nt.ValidateNotFound(configsync.RepoSyncName, namespace, &v1beta1.RepoSync{}); err != nil {
		nt.T.Error(err)
	}
	// Verify the default sourceType is set when not specified.
	nt.T.Log("Manually apply the RepoSync object")
	nt.MustKubectl("apply", "-f", rsGitYAMLFile)
	if err := nt.Validate(configsync.RepoSyncName, namespace, &v1beta1.RepoSync{}, isSourceType(v1beta1.GitSource)); err != nil {
		nt.T.Error(err)
	}
	nt.T.Log("Verify the namespace objects are synced")
	nt.WaitForSync(kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, namespace,
		nt.DefaultWaitTimeout, nomostest.RemoteNsRepoSha1Fn, nomostest.RepoSyncHasStatusSyncCommit, nil)
	if err := nt.Validate("bookinfo-sa", namespace, &corev1.ServiceAccount{},
		nomostest.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.ValidateNotFound("bookinfo-admin", namespace, &rbacv1.Role{}); err != nil {
		nt.T.Error(err)
	}
	// Switch from Git to OCI
	nt.T.Log("Manually update the RepoSync object to sync from OCI")
	nt.MustKubectl("apply", "-f", rsOCIYAMLFile)
	if err := nt.Validate(configsync.RepoSyncName, namespace, &v1beta1.RepoSync{}, isSourceType(v1beta1.OciSource)); err != nil {
		nt.T.Error(err)
	}
	nt.T.Log("Verify the namespace objects are synced")
	nt.WaitForSync(kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, namespace,
		nt.DefaultWaitTimeout, nsRepoOCIDigestShaFunc, nomostest.RepoSyncHasStatusSyncCommit, nil)
	if err := nt.Validate("bookinfo-admin", namespace, &rbacv1.Role{},
		nomostest.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.ValidateNotFound("bookinfo-sa", namespace, &corev1.ServiceAccount{}); err != nil {
		nt.T.Error(err)
	}

	// Invalid cases
	rs := fake.RepoSyncObjectV1Beta1(namespace, configsync.RepoSyncName)
	nt.T.Log("Manually patch RepoSync object to miss Git spec when sourceType is git")
	nt.MustMergePatch(rs, `{"spec":{"sourceType":"git", "git":null, "oci": null}}`)
	nt.WaitForRepoSyncStalledError(namespace, configsync.RepoSyncName, "Validation", `KNV1061: RepoSyncs must specify spec.git when spec.sourceType is "git"`)
	nt.T.Log("Manually patch RepoSync object to miss OCI spec when sourceType is oci")
	nt.MustMergePatch(rs, `{"spec":{"sourceType":"oci"}}`)
	nt.WaitForRepoSyncStalledError(namespace, configsync.RepoSyncName, "Validation", `KNV1061: RepoSyncs must specify spec.oci when spec.sourceType is "oci"`)
}

// resourceQuotaHasHardPods validates if the RepoSync has the expected sourceType.
func isSourceType(sourceType v1beta1.SourceType) nomostest.Predicate {
	return func(o client.Object) error {
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return nomostest.WrongTypeErr(rs, &v1beta1.RepoSync{})
		}
		actual := rs.Spec.SourceType
		if string(sourceType) != actual {
			return fmt.Errorf("RepoSync sourceType %q is not equal to the expected %q", actual, sourceType)
		}
		return nil
	}
}

/*
// TestDigestUpdateInAR tests if the oci-sync container can pull new digests with the same tag.
// The test requires permission to push new image to `config-sync-ci-public` in the Artifact Registry,
// and permission to push new image to `config-sync-ci-public` in the Container Registry.
// The test uses the current credentials (gcloud auth) when running on the GKE clusters to push new images.
func TestDigestUpdate(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured, ntopts.RequireGKE(t))
	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
			v1beta1.GitSource, origRepoURL))
	})

	nt.T.Log("Test oci-sync pulling the latest image from AR when digest changes")
	testDigestUpdate(nt, "us-docker.pkg.dev/stolos-dev/config-sync-ci-public/digest-update")

	nt.T.Log("Test oci-sync pulling the latest image from GCR when digest changes")
	testDigestUpdate(nt, "gcr.io/stolos-dev/config-sync-ci/digest-update")
}

func testDigestUpdate(nt *nomostest.NT, image string) {
	auth := remote.WithAuthFromKeychain(gcrane.Keychain)
	packagePath := "../testdata/hydration/kustomize-components"
	digest, err := archiveAndPushOCIImage(image, packagePath, auth)
	if err != nil {
		nt.T.Fatal(err)
	}

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a public OCI image")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"image": "%s", "auth": "none"}, "git": null}}`,
		v1beta1.OciSource, image))
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(fixedOCIDigest(digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "."}))
	validateAllTenants(nt, string(declared.RootReconciler), "base", "tenant-a", "tenant-b", "tenant-c")

	nt.T.Log("Publish new content to the image with the same tag")
	packagePath = "../testdata/hydration/helm-components"
	newDigest, err := archiveAndPushOCIImage(image, packagePath, auth)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(fixedOCIDigest(newDigest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "."}))
	validateHelmComponents(nt, string(declared.RootReconciler))
}
*/

func fixedOCIDigest(digest string) nomostest.Sha1Func {
	return func(*nomostest.NT, types.NamespacedName) (string, error) {
		return digest, nil
	}
}

/*
// archiveAndPushOCIImage tars and extracts (untar) image files to target directory.
// The desired version or digest must be in the imageName, and the resolved image sha256 digest is returned.
// It is used for testing only.
func archiveAndPushOCIImage(imageName string, dir string, options ...remote.Option) (string, error) {
	ref, err := name.ParseReference(imageName)
	if err != nil {
		return "", fmt.Errorf("failed to parse reference %q: %v", imageName, err)
	}

	// Make new layer
	tarFile, err := ioutil.TempFile("", "tar")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = os.Remove(tarFile.Name())
	}()

	if err := func() error {
		defer func() {
			_ = tarFile.Close()
		}()

		gw := gzip.NewWriter(tarFile)
		defer func() {
			_ = gw.Close()
		}()

		tw := tar.NewWriter(gw)
		defer func() {
			_ = tw.Close()
		}()

		if err := filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}

			relative, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			if info.IsDir() && relative == "." {
				return nil
			}

			// TODO if info is symlink also read link target
			link := ""

			// generate tar header
			header, err := tar.FileInfoHeader(info, link)
			if err != nil {
				return err
			}

			// must provide real name
			// (see https://golang.org/src/archive/tar/common.go?#L626)
			header.Name = filepath.ToSlash(relative)

			var buf *bytes.Buffer
			// write header
			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			// if not a dir, write file content
			if !info.IsDir() {
				data, err := os.Open(path)
				if err != nil {
					return err
				}
				if buf != nil {
					if _, err := io.Copy(tw, buf); err != nil {
						return err
					}
				} else {
					if _, err := io.Copy(tw, data); err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}

		return nil
	}(); err != nil {
		return "", err
	}

	// Append new layer
	newLayers := []string{tarFile.Name()}
	img, err := crane.Append(empty.Image, newLayers...)
	if err != nil {
		return "", fmt.Errorf("failed to append %v: %v", newLayers, err)
	}

	if err := remote.Write(ref, img, options...); err != nil {
		return "", fmt.Errorf("pushing image %s: %v", ref, err)
	}

	// Determine the digest of the image that was pushed
	imageDigestHash, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("failed to calculate image digest: %w", err)
	}
	return imageDigestHash.Hex, nil
}
*/
