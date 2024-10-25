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
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reposync"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	nt.T.Cleanup(func() {
		if err := nomostest.TeardownOCISignatureVerification(nt); err != nil {
			nt.T.Error(err)
		}
		if t.Failed() {
			nt.PodLogs(nomostest.OCISignatureVerificationNamespace, nomostest.OCISignatureVerificationServerName, "", false)
		}
	})
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
		testwatcher.WatchPredicates(testpredicates.RepoSyncHasSourceError(status.SourceErrorCode, "no signatures found"))))

	nt.T.Log("Create second image with latest tag.")
	bookstoreSA := k8sobjects.ServiceAccountObject("bookstore-sa")
	image1, err := nt.BuildAndPushOCIImage(repoSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookInfoRole, bookstoreSA))
	if err != nil {
		nt.T.Fatal(err)
	}

	imageURL, err := image.RemoteAddressWithDigest()
	if err != nil {
		nt.T.Fatal("Failed to get first image remote URL %s", err)
	}
	nt.T.Logf("Signing first test image %s", imageURL)
	_, err = nt.Shell.ExecWithDebugEnv("cosign", cosignPassword, "sign", imageURL, "--key", nomostest.CosignPrivateKeyPath(nt), "--yes")
	if err != nil {
		nt.T.Fatalf("Failed to sign first test image %s", err)
	}

	nt.T.Log("Check that verification is up-to-date with floating tag")
	imageURL1, err := image1.RemoteAddressWithDigest()
	if err != nil {
		nt.T.Fatal("Failed to get second image remote URL %s", err)
	}
	nt.T.Logf("Checking no signature error exists for second image with digest %s", image1.Digest)
	nt.Must(
		nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, namespaceRepo,
			testwatcher.WatchPredicates(testpredicates.RepoSyncHasSourceError(status.SourceErrorCode, "no signatures found"))),
		nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, namespaceRepo,
			testwatcher.WatchPredicates(testpredicates.RepoSyncHasSourceError(status.SourceErrorCode, image1.Digest))))

	nt.T.Logf("Signing second test image %s", imageURL1)
	_, err = nt.Shell.ExecWithDebugEnv("cosign", cosignPassword, "sign", imageURL1, "--key", nomostest.CosignPrivateKeyPath(nt), "--yes")
	if err != nil {
		nt.T.Fatalf("Failed to sign second test image %s", err)
	}
	nt.T.Log("Update source expectation for watch")
	_ = nt.RepoSyncObjectOCI(repoSyncKey, image.OCIImageID().WithoutDigest(), "", image1.Digest)
	nt.Must(nt.WatchForAllSyncs())
	err = nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, repoSyncID.Namespace,
		testwatcher.WatchPredicates(
			checkRepoSyncPreSyncAnnotations(image1),
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
	if err := nomostest.TeardownOCISignatureVerification(nt); err != nil {
		nt.T.Fatal(err)
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
	nt.T.Cleanup(func() {
		if err := nomostest.TeardownOCISignatureVerification(nt); err != nil {
			nt.T.Error(err)
		}
		if t.Failed() {
			nt.PodLogs(nomostest.OCISignatureVerificationNamespace, nomostest.OCISignatureVerificationServerName, "", false)
		}
	})
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
		testwatcher.WatchPredicates(testpredicates.RootSyncHasSourceError(status.SourceErrorCode, "no signatures found"))))

	nt.T.Log("Create second image with latest tag.")
	bookstoreSA := k8sobjects.ServiceAccountObject("bookstore-sa")
	image1, err := nt.BuildAndPushOCIImage(rootSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookInfoRole, bookstoreSA))
	if err != nil {
		nt.T.Fatal(err)
	}

	imageURL, err := image.RemoteAddressWithDigest()
	if err != nil {
		nt.T.Fatal("Failed to get first image remote URL %s", err)
	}
	nt.T.Logf("Signing first test image %s", imageURL)
	_, err = nt.Shell.ExecWithDebugEnv("cosign", cosignPassword, "sign", imageURL, "--key", nomostest.CosignPrivateKeyPath(nt), "--yes")
	if err != nil {
		nt.T.Fatalf("Failed to sign first test image %s", err)
	}

	nt.T.Log("Check that verification is up-to-date with floating tag")
	imageURL1, err := image1.RemoteAddressWithDigest()
	if err != nil {
		nt.T.Fatal("Failed to get second image remote URL %s", err)
	}
	nt.T.Logf("Checking no signature error exists for second image with digest %s", image1.Digest)
	nt.Must(
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, configsync.ControllerNamespace,
			testwatcher.WatchPredicates(testpredicates.RootSyncHasSourceError(status.SourceErrorCode, "no signatures found"))),
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, configsync.ControllerNamespace,
			testwatcher.WatchPredicates(testpredicates.RootSyncHasSourceError(status.SourceErrorCode, image1.Digest))))

	nt.T.Logf("Signing second test image %s", imageURL1)
	_, err = nt.Shell.ExecWithDebugEnv("cosign", cosignPassword, "sign", imageURL1, "--key", nomostest.CosignPrivateKeyPath(nt), "--yes")
	if err != nil {
		nt.T.Fatalf("Failed to sign second test image %s", err)
	}
	nt.T.Log("Update source expectation for watch")
	_ = nt.RootSyncObjectOCI(rootSyncKey.Name, image.OCIImageID().WithoutDigest(), "", image1.Digest)
	nt.Must(nt.WatchForAllSyncs())
	nt.T.Log("Check RootSync is synced to second image and annotations are updated.")
	err = nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			checkRootSyncPreSyncAnnotations(image1),
		))
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
	if err := nomostest.TeardownOCISignatureVerification(nt); err != nil {
		nt.T.Fatal(err)
	}
}

func checkRootSyncPreSyncAnnotations(image *registryproviders.OCIImage) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1beta1.RootSync{})
		}
		syncingCondition := rootsync.GetCondition(rs.Status.Conditions, v1beta1.RootSyncSyncing)
		syncingCommit := syncingCondition.Commit
		syncingImage := rs.Spec.Oci.Image
		imageToSyncAnnotation, ok := o.GetAnnotations()[metadata.ImageToSyncAnnotationKey]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q", o.GetName(), metadata.ImageToSyncAnnotationKey)
		}
		remoteAddr := image.OCIImageID().Address()
		if imageToSyncAnnotation != fmt.Sprintf("%s@sha256:%s", syncingImage, syncingCommit) || imageToSyncAnnotation != remoteAddr {
			return fmt.Errorf("expecting to have OCI image %s, but got image-to-sync annotation %s, syncing commit %s, syncing image %s",
				remoteAddr,
				imageToSyncAnnotation,
				syncingCommit,
				syncingImage,
			)
		}
		return nil
	}
}

func checkRepoSyncPreSyncAnnotations(image *registryproviders.OCIImage) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RepoSync)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1beta1.RepoSync{})
		}
		syncingCondition := reposync.GetCondition(rs.Status.Conditions, v1beta1.RepoSyncSyncing)
		syncingCommit := syncingCondition.Commit
		syncingImage := rs.Spec.Oci.Image
		imageToSyncAnnotation, ok := o.GetAnnotations()[metadata.ImageToSyncAnnotationKey]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q", o.GetName(), metadata.ImageToSyncAnnotationKey)
		}
		remoteAddr := image.OCIImageID().Address()
		if imageToSyncAnnotation != fmt.Sprintf("%s@sha256:%s", syncingImage, syncingCommit) || imageToSyncAnnotation != remoteAddr {
			return fmt.Errorf("expecting to have OCI image %s, but got image-to-sync annotation %s, syncing commit %s, syncing image %s",
				remoteAddr,
				imageToSyncAnnotation,
				syncingCommit,
				syncingImage,
			)
		}
		return nil
	}
}
