// Copyright 2024 Google LLC
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

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
)

var cosignPassword = map[string]string{
	"COSIGN_PASSWORD": "test",
}

func TestAddPreSyncAnnotationRepoSync(t *testing.T) {
	repoSyncID := core.RepoSyncID(configsync.RepoSyncName, namespaceRepo)
	nt := nomostest.New(t, nomostesting.SyncSource,
		ntopts.RequireOCIProvider,
		ntopts.SyncWithGitSource(repoSyncID),
		ntopts.RepoSyncPermissions(policy.RBACAdmin(), policy.CoreAdmin()))

	if err := nomostest.SetupOCISignatureVerification(nt); err != nil {
		nt.T.Fatal(err)
	}
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	repoSyncKey := repoSyncID.ObjectKey
	gitSource := nt.SyncSources[repoSyncID]

	nt.T.Log("Create first OCI image with latest tag.")
	bookInfoRole := k8sobjects.RoleObject(core.Name("book-info-admin"))
	image, err := nt.BuildAndPushOCIImage(repoSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookInfoRole))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Create RepoSync with first OCI image")
	repoSyncOCI := nt.RepoSyncObjectOCI(repoSyncKey, image.OCIImageID().WithoutDigest(), "", image.Digest)
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncOCI))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the RepoSync to sync from OCI"))
	nt.Must(nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, namespaceRepo,
		testwatcher.WatchPredicates(testpredicates.RepoSyncHasSourceError(status.APIServerErrorCode, "no signatures found"))))

	nt.T.Log("Create second image with latest tag.")
	bookstoreSA := k8sobjects.ServiceAccountObject("bookstore-sa")
	image1, err := nt.BuildAndPushOCIImage(repoSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookInfoRole, bookstoreSA))
	if err != nil {
		nt.T.Fatal(err)
	}

	if err = signImage(nt, image); err != nil {
		nt.T.Fatalf("Failed to sign first test image %s", err)
	}

	nt.T.Log("Check that verification is up-to-date with floating tag")
	nt.T.Logf("Checking no signature error exists for second image with digest %s", image1.Digest)
	nt.Must(
		nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, namespaceRepo,
			testwatcher.WatchPredicates(
				testpredicates.RepoSyncHasSourceError(status.APIServerErrorCode, "no signatures found"),
				testpredicates.RepoSyncHasSourceError(status.APIServerErrorCode, image1.Digest))))

	if err = signImage(nt, image1); err != nil {
		nt.T.Fatalf("Failed to sign second test image %s", err)
	}
	nt.T.Log("Update source expectation for watch")
	_ = nt.RepoSyncObjectOCI(repoSyncKey, image.OCIImageID().WithoutDigest(), "", image1.Digest)
	nt.Must(nt.WatchForAllSyncs())
	err = nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, repoSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.ImageToSyncAnnotationKey, image1.OCIImageID().Address()),
		))
	if err != nil {
		nt.T.Fatalf("Source annotation not updated for RepoSync %v", err)
	}

	nt.T.Log("Set the RepoSync to sync from Git")
	nt.SyncSources[repoSyncID] = gitSource
	repoSyncGit := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncID.ObjectKey)
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncGit))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the RepoSync to sync from Git"))
	nt.Must(nt.WatchForAllSyncs())

	err = nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, repoSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.MissingAnnotation(metadata.ImageToSyncAnnotationKey),
		))
	if err != nil {
		nt.T.Fatalf("Source annotations still exist when RepoSync is syncing from Git %v", err)
	}
}

func TestAddPreSyncAnnotationRootSync(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncKey := rootSyncID.ObjectKey
	nt := nomostest.New(t, nomostesting.SyncSource,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
		ntopts.RequireOCIProvider,
	)

	if err := nomostest.SetupOCISignatureVerification(nt); err != nil {
		nt.T.Fatal(err)
	}

	gitSource := nt.SyncSources[rootSyncID]

	nt.T.Log("Create first OCI image with latest tag.")
	bookInfoRole := k8sobjects.RoleObject(core.Name("book-info-admin"))
	image, err := nt.BuildAndPushOCIImage(rootSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookInfoRole))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Create RootSync with first OCI image")
	rootSyncOCI := nt.RootSyncObjectOCI(rootSyncKey.Name, image.OCIImageID().WithoutDigest(), "", image.Digest)
	nt.Must(nt.KubeClient.Apply(rootSyncOCI))
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, configsync.ControllerNamespace,
		testwatcher.WatchPredicates(testpredicates.RootSyncHasSourceError(status.APIServerErrorCode, "no signatures found"))))

	nt.T.Log("Create second image with latest tag.")
	bookstoreSA := k8sobjects.ServiceAccountObject("bookstore-sa")
	image1, err := nt.BuildAndPushOCIImage(rootSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookInfoRole, bookstoreSA))
	if err != nil {
		nt.T.Fatal(err)
	}

	if err = signImage(nt, image); err != nil {
		nt.T.Fatalf("Failed to sign first test image %s", err)
	}

	nt.T.Log("Check that verification is up-to-date with floating tag")
	nt.T.Logf("Checking no signature error exists for second image with digest %s", image1.Digest)
	nt.Must(
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, configsync.ControllerNamespace,
			testwatcher.WatchPredicates(
				testpredicates.RootSyncHasSourceError(status.APIServerErrorCode, "no signatures found"),
				testpredicates.RootSyncHasSourceError(status.APIServerErrorCode, image1.Digest))))

	if err = signImage(nt, image1); err != nil {
		nt.T.Fatalf("Failed to sign second test image %s", err)
	}
	nt.T.Log("Update source expectation for watch")
	_ = nt.RootSyncObjectOCI(rootSyncKey.Name, image.OCIImageID().WithoutDigest(), "", image1.Digest)
	nt.Must(nt.WatchForAllSyncs())
	nt.T.Log("Check RootSync is synced to second image and annotations are updated.")
	err = nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.ImageToSyncAnnotationKey, image1.OCIImageID().Address())))
	if err != nil {
		nt.T.Fatalf("Source annotation not updated for RootSync %v", err)
	}

	nt.T.Log("Set the RootSync to sync from Git")
	nt.SyncSources[rootSyncID] = gitSource
	rootSyncGit := nomostest.RootSyncObjectV1Beta1FromRootRepo(nt, rootSyncID.Name)
	nt.Must(nt.KubeClient.Apply(rootSyncGit))
	nt.Must(nt.WatchForAllSyncs())

	err = nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.MissingAnnotation(metadata.ImageToSyncAnnotationKey),
		))
	if err != nil {
		nt.T.Fatalf("Source annotations still exist when RootSync is syncing from Git %v", err)
	}
}

func signImage(nt *nomostest.NT, image *registryproviders.OCIImage) error {
	imageURL, err := image.LocalAddressWithDigest()
	if err != nil {
		return fmt.Errorf("getting image remote URL %s", err)
	}

	nt.T.Logf("Signing test image %s", imageURL)
	_, err = nt.OCIClient.Shell.ExecWithDebugEnv("cosign", cosignPassword, "sign",
		imageURL, "--key", nomostest.CosignPrivateKeyPath(nt), "--yes")
	return err
}
