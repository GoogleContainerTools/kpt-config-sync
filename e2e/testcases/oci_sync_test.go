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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/gcrane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/testing/fake"
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
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "unstructured", "sourceType": "%s", "oci": {"image": "%s", "auth": "none"}, "git": null}}`,
		v1beta1.OciSource, publicARImage))
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "hierarchy", "sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
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
//    - `roles/artifactregistry.reader` for access image in Artifact Registry.
//    - `roles/containerregistry.ServiceAgent` for access image in Container Registry.
func TestGCENodeOCI(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured,
		ntopts.RequireGKE(t), ntopts.GCENodeTest)

	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	tenant := "tenant-a"

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from an OCI image in Artifact Registry")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "unstructured", "sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcenode"}, "git": null}}`,
		v1beta1.OciSource, tenant, privateARImage))
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "hierarchy", "sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
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

// TestOCIWorkloadIdentity tests the `gcpserviceaccount` auth type with both GKE
// Workload Identity and Fleet Workload Identity (in-project and cross-project).
//  The test will run on a GKE cluster only with following pre-requisites
// 1. Workload Identity is enabled.
// 2. The Google service account `e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/artifactregistry.reader` for access image in Artifact Registry.
// 3. The Google service account `e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com` is created with `roles/containerregistry.ServiceAgent` for access image in Container Registry.
// 4. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//  gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//     --role roles/iam.workloadIdentityUser \
//     --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//     --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//     e2e-test-ar-reader@stolos-dev.iam.gserviceaccount.com
// 5. An IAM policy binding is created between the Google service account and the Kubernetes service accounts with the `roles/iam.workloadIdentityUser` role.
//   gcloud iam service-accounts add-iam-policy-binding --project=stolos-dev \
//      --role roles/iam.workloadIdentityUser \
//      --member "serviceAccount:stolos-dev.svc.id.goog[config-management-system/root-reconciler]" \
//      --member="serviceAccount:cs-dev-hub.svc.id.goog[config-management-system/root-reconciler]" \
//      e2e-test-gcr-reader@stolos-dev.iam.gserviceaccount.com
// 6. The cross-project fleet host project 'cs-dev-hub' is created.
// 7. The following environment variables are set: GCP_PROJECT, GCP_CLUSTER, GCP_REGION|GCP_ZONE.
func TestOCIWorkloadIdentity(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured, ntopts.RequireGKE(t))

	origRepoURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	crossProjectFleetProjectID := "cs-dev-hub"
	gcpProject := os.Getenv("GCP_PROJECT")
	if gcpProject == "" {
		t.Fatal("Environment variable 'GCP_PROJECT' is required for this test case")
	}
	gcpCluster := os.Getenv("GCP_CLUSTER")
	if gcpCluster == "" {
		t.Fatal("Environment variable 'GCP_CLUSTER' is required for this test case")
	}
	gkeRegion := os.Getenv("GCP_REGION")
	gkeZone := os.Getenv("GCP_ZONE")
	if gkeRegion == "" && gkeZone == "" {
		t.Fatal("Environment variable 'GCP_REGION' or 'GCP_ZONE' is required for this test case")
	}
	fleetMembership := fmt.Sprintf("%s-%s", gcpProject, gcpCluster)
	gkeURI := "https://container.googleapis.com/v1/projects/" + gcpProject
	if gkeRegion != "" {
		gkeURI += fmt.Sprintf("/locations/%s/clusters/%s", gkeRegion, gcpCluster)
	} else {
		gkeURI += fmt.Sprintf("/zones/%s/clusters/%s", gkeZone, gcpCluster)
	}

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Cleanup(func() {
		// Change the rs back so that it works in the shared test environment.
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "hierarchy", "sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
			v1beta1.GitSource, origRepoURL))
		// Unregister the cluster in the same project.
		if err := unregisterCluster(fleetMembership, gcpProject, gkeURI); err != nil {
			nt.T.Log(err)
		}
		// Unregister the cluster in a different fleet host project.
		if err := unregisterCluster(fleetMembership, crossProjectFleetProjectID, gkeURI); err != nil {
			nt.T.Log(err)
		}
	})

	nt.T.Log("Test OCI Workload Identity for AR")
	testWorkloadIdentity(nt, fleetMembership, gcpProject, crossProjectFleetProjectID, gkeURI, privateARImage, gsaARReaderEmail)

	nt.T.Log("Test OCI Workload Identity for GCR")
	testWorkloadIdentity(nt, fleetMembership, gcpProject, crossProjectFleetProjectID, gkeURI, privateGCRImage, gsaGCRReaderEmail)
}

func testWorkloadIdentity(nt *nomostest.NT, fleetMembership, gcpProject, crossProjectFleetProjectID, gkeURI, image, gsaEmail string) {
	tenant := "tenant-a"
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from an OCI image")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "unstructured", "sourceType": "%s", "oci": {"dir": "%s", "image": "%s", "auth": "gcpserviceaccount", "gcpServiceAccountEmail": "%s"}, "git": null}}`,
		v1beta1.OciSource, tenant, image, gsaEmail))

	nt.T.Log("Verify GKE workload identity")
	nt.T.Log("Unregister the cluster if it is registered")
	if err := unregisterCluster(fleetMembership, gcpProject, gkeURI); err != nil {
		nt.T.Log(err)
	}

	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(fixedOCIDigest(imageDigest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: tenant}))
	validateAllTenants(nt, string(declared.RootReconciler), "../base", tenant)
	validateFWICredentials(nt, nomostest.DefaultRootReconcilerName, fwiAnnotationAbsent)
}

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
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "hierarchy", "sourceType": "%s", "oci": null, "git": {"dir": "acme", "branch": "main", "repo": "%s", "auth": "ssh","gcpServiceAccountEmail": "", "secretRef": {"name": "git-creds"}}}}`,
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
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceFormat": "unstructured", "sourceType": "%s", "oci": {"image": "%s", "auth": "none"}, "git": null}}`,
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

func fixedOCIDigest(digest string) nomostest.Sha1Func {
	return func(*nomostest.NT, types.NamespacedName) (string, error) {
		return digest, nil
	}
}

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
