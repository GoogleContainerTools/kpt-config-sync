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
	"os"
	"os/exec"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/kustomizecomponents"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// All the following images were built from the directory e2e/testdata/hydration/kustomize-components,
// which includes a kustomization.yaml file in the root directory that
// references resources for tenant-a, tenant-b, and tenant-c.
// Each tenant includes a NetworkPolicy, a Role and a RoleBinding.

const (
	// publicGCRImage pulls the public OCI image by the default `latest` tag
	// The test-infra GCR is public.
	publicGCRImage = nomostesting.TestInfraContainerRegistry + "/kustomize-components"

	// publicARImage pulls the public OCI image by the default `latest` tag
	publicARImage = nomostesting.ConfigSyncTestPublicRegistry + "/kustomize-components"
)

// privateGCRImage pulls the private OCI image by tag
// The test environment GCR is assumed to be private.
func privateGCRImage(sourcePackage string) string {
	return fmt.Sprintf("%s/%s/config-sync-test/%s:v1", nomostesting.GCRHost, *e2e.GCPProject, sourcePackage)
}

func gsaARReaderEmail() string {
	return fmt.Sprintf("e2e-test-ar-reader@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

func gsaGCRReaderEmail() string {
	return fmt.Sprintf("e2e-test-gcr-reader@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

// TestPublicOCI can run on both Kind and GKE clusters.
// It tests Config Sync can pull from public OCI images without any authentication.
func TestPublicOCI(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.T.Log("Update RootSync to sync from a public OCI image in AR")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"image": "%s", "auth": "none"}, "git": null}}`,
		configsync.OciSource, publicARImage))
	err := nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByName(publicARImage)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: ".",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "base", "tenant-a", "tenant-b", "tenant-c")

	tenant := "tenant-a"
	nt.T.Logf("Update RootSync to sync %s from a public OCI image in GCR", tenant)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"oci": {"image": "%s", "dir": "%s"}}}`, publicGCRImage, tenant))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByName(publicGCRImage)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: "tenant-a",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	kustomizecomponents.ValidateAllTenants(nt, string(declared.RootScope), "../base", tenant)
}

func TestSwitchFromGitToOciCentralized(t *testing.T) {
	namespace := testNs
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireOCIProvider,
		ntopts.NamespaceRepo(namespace, configsync.RepoSyncName),
		// bookinfo image contains RoleBinding
		// bookinfo repo contains ServiceAccount
		ntopts.RepoSyncPermissions(policy.RBACAdmin(), policy.CoreAdmin()),
	)
	var err error
	// file path to the RepoSync config in the root repository.
	repoSyncPath := nomostest.StructuredNSPath(namespace, configsync.RepoSyncName)
	rsNN := types.NamespacedName{
		Name:      configsync.RepoSyncName,
		Namespace: namespace,
	}

	// Remote git branch will only contain the bookinfo-sa ServiceAccount
	bookinfoSA := fake.ServiceAccountObject("bookinfo-sa", core.Namespace(namespace))
	nt.Must(nt.NonRootRepos[rsNN].Add("acme/sa.yaml", bookinfoSA))
	nt.Must(nt.NonRootRepos[rsNN].CommitAndPush("Add ServiceAccount"))

	// OCI image will only contain the bookinfo-admin role
	bookinfoRole := fake.RoleObject(core.Name("bookinfo-admin"))
	image, err := nt.BuildAndPushOCIImage(rsNN, registryproviders.ImageInputObjects(nt.Scheme, bookinfoRole))
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(bookinfoSA.Name, bookinfoSA.Namespace, &corev1.ServiceAccount{},
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(bookinfoRole.Name, namespace, &rbacv1.Role{}); err != nil {
		nt.T.Fatal(err)
	}

	// Switch from Git to OCI
	nt.T.Log("Update the RepoSync object to sync from OCI")
	repoSyncOCI := nt.RepoSyncObjectOCI(rsNN, image)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(repoSyncPath, repoSyncOCI))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("configure RepoSync to sync from OCI in the root repository"))

	if err := nt.WatchForAllSyncs(
		nomostest.WithRepoSha1Func(imageDigestFuncByDigest(image.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			rsNN: ".",
		})); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(configsync.RepoSyncName, namespace, &v1beta1.RepoSync{}, isSourceType(configsync.OciSource)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.Validate(bookinfoRole.Name, namespace, &rbacv1.Role{},
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Error(err)
	}
	if err := nt.ValidateNotFound(bookinfoSA.Name, namespace, &corev1.ServiceAccount{}); err != nil {
		nt.T.Error(err)
	}
}

func TestSwitchFromGitToOciDelegated(t *testing.T) {
	namespace := testNs
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.WithDelegatedControl, ntopts.RequireOCIProvider,
		ntopts.NamespaceRepo(namespace, configsync.RepoSyncName),
		// bookinfo image contains RoleBinding
		// bookinfo repo contains ServiceAccount
		ntopts.RepoSyncPermissions(policy.RBACAdmin(), policy.CoreAdmin()),
	)
	rsNN := types.NamespacedName{
		Name:      configsync.RepoSyncName,
		Namespace: namespace,
	}

	// Remote git branch will only contain the bookinfo-sa ServiceAccount
	bookinfoSA := fake.ServiceAccountObject("bookinfo-sa", core.Namespace(namespace))
	nt.Must(nt.NonRootRepos[rsNN].Add("acme/sa.yaml", bookinfoSA))
	nt.Must(nt.NonRootRepos[rsNN].CommitAndPush("Add ServiceAccount"))

	// OCI image will only contain the bookinfo-admin role
	bookinfoRole := fake.RoleObject(core.Name("bookinfo-admin"))
	image, err := nt.BuildAndPushOCIImage(rsNN, registryproviders.ImageInputObjects(nt.Scheme, bookinfoRole))
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Verify the manual configuration: switch from Git to OCI
	// Verify the default sourceType is set when not specified.
	if err := nt.Validate(bookinfoSA.Name, namespace, &corev1.ServiceAccount{},
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(bookinfoRole.Name, namespace, &rbacv1.Role{}); err != nil {
		nt.T.Fatal(err)
	}

	// Switch from Git to OCI
	repoSyncOCI := nt.RepoSyncObjectOCI(rsNN, image)
	nt.T.Log("Manually update the RepoSync object to sync from OCI")
	if err := nt.KubeClient.Apply(repoSyncOCI); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(configsync.RepoSyncName, namespace, &v1beta1.RepoSync{}, isSourceType(configsync.OciSource)); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the namespace objects are synced")
	err = nt.WatchForSync(kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, namespace,
		imageDigestFuncByDigest(image.Digest), nomostest.RepoSyncHasStatusSyncCommit, nil)
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate(bookinfoRole.Name, namespace, &rbacv1.Role{},
		testpredicates.HasAnnotation(metadata.ResourceManagerKey, namespace)); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound(bookinfoSA.Name, namespace, &corev1.ServiceAccount{}); err != nil {
		nt.T.Fatal(err)
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
func isSourceType(sourceType configsync.SourceType) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return testpredicates.WrongTypeErr(rs, &v1beta1.RepoSync{})
		}
		actual := rs.Spec.SourceType
		if sourceType != actual {
			return fmt.Errorf("RepoSync sourceType %q is not equal to the expected %q", actual, sourceType)
		}
		return nil
	}
}

func TestOCIFloatingTag(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireOCIProvider,
	)
	rsNN := nomostest.RootSyncNN(configsync.RootSyncName)

	nt.T.Log("create a Namespace with an invalid name")
	invalidNamespace := fake.NamespaceObject("bob-%")
	// OCI image will only contain the namespace with invalid name
	image, err := nt.BuildAndPushOCIImage(rsNN, registryproviders.ImageInputObjects(nt.Scheme, invalidNamespace))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSyncOCI := nt.RootSyncObjectOCI(rsNN.Name, image)
	nt.Must(nt.KubeClient.Apply(rootSyncOCI))

	nt.WaitForRootSyncSourceError(rsNN.Name, "1036", "consists of lower case alphanumeric characters", nil)

	// Push a new image with the same tag which contains a valid Namespace
	validNamespace := fake.NamespaceObject("bobby")
	image, err = nt.BuildAndPushOCIImage(rsNN, registryproviders.ImageInputObjects(nt.Scheme, validNamespace))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the namespace objects are synced")
	nt.Must(nt.WatchForSync(kinds.RootSyncV1Beta1(), rsNN.Name, rsNN.Namespace,
		imageDigestFuncByDigest(image.Digest), nomostest.RootSyncHasStatusSyncCommit, nil))
	nt.Must(nt.Validate(validNamespace.Name, "", &corev1.Namespace{}))
}

/*
// TestDigestUpdateInAR tests if the oci-sync container can pull new digests with the same tag.
// The test requires permission to push new image to `config-sync-test-public` in the Artifact Registry,
// and permission to push new image to `config-sync-test-public` in the Container Registry.
// The test uses the current credentials (gcloud auth) when running on the GKE clusters to push new images.
func TestDigestUpdate(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured, ntopts.RequireGKE(t))

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)

	nt.T.Log("Test oci-sync pulling the latest image from AR when digest changes")
	testDigestUpdate(nt, "us-docker.pkg.dev/${GCP_PROJECT}/config-sync-test-public/digest-update")

	nt.T.Log("Test oci-sync pulling the latest image from GCR when digest changes")
	testDigestUpdate(nt, "gcr.io/${GCP_PROJECT}/config-sync-test/digest-update")
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

// getImageDigest uses gcloud to read the image digest of the specified image.
// Using gcloud, instead of the OCI SDK, allows authenticating with local user
// auth OR default app credentials. gcloud can authenticate with Google Artifact
// Registry and Google Container Registry or pull with anonymous auth.
// This allows reading the image digest without pulling the whole image.
// Requires a sha256 image digest.
func getImageDigest(nt *nomostest.NT, imageName string) (string, error) {
	args := []string{
		"gcloud", "container", "images", "describe",
		imageName,
		"--format", "value(image_summary.digest)",
		"--verbosity", "error", // hide the warning about using "latest"
	}
	nt.Logger.Debug(strings.Join(args, " "))
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		nt.T.Log(string(out))
		return "", err
	}
	hex, found := cutPrefix(strings.TrimSpace(string(out)), "sha256:")
	if !found {
		return "", fmt.Errorf("image %q has invalid digest %q", imageName, string(out))
	}
	return hex, nil
}

// imageDigestFuncByName wraps getImageDigest to return a Sha1Func that caches the
// image digest, to avoid polling the image registry unnecessarily.
func imageDigestFuncByName(imageName string) nomostest.Sha1Func {
	var cached bool
	var digest string
	var err error
	return func(nt *nomostest.NT, _ types.NamespacedName) (string, error) {
		if cached {
			return digest, err
		}
		digest, err = getImageDigest(nt, imageName)
		cached = true
		return digest, err
	}
}

// imageDigestFuncByDigest uses the provided digest to return a valid Sha1Func
func imageDigestFuncByDigest(digest string) func(nt *nomostest.NT, nn types.NamespacedName) (string, error) {
	return func(_ *nomostest.NT, _ types.NamespacedName) (string, error) {
		// The RSync status does not include the sha256: prefix
		return strings.TrimPrefix(digest, "sha256:"), nil
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
	tarFile, err := os.CreateTemp("", "tar")
	if err != nil {
		return "", err
	}
	defer func() {
		nt.Must(_ = os.Remove(tarFile.Name()))
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
					nt.Must(if _, err := io.Copy(tw, buf)); err != nil {
						return err
					}
				} else {
					nt.Must(if _, err := io.Copy(tw, data)); err != nil {
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

// cutPrefix is like strings.TrimPrefix, but also returns whether the prefix was
// found or not. Backported from Go 1.20.
func cutPrefix(s, prefix string) (after string, found bool) {
	if !strings.HasPrefix(s, prefix) {
		return s, false
	}
	return s[len(prefix):], true
}
