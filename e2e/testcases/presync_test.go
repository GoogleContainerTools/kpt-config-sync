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
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAddPreSyncAnnotationRepoSync(t *testing.T) {
	repoSyncID := core.RepoSyncID(configsync.RepoSyncName, namespaceRepo)
	nt := nomostest.New(t, nomostesting.SyncSource,
		ntopts.RequireOCIProvider,
		ntopts.SyncWithGitSource(repoSyncID),
		ntopts.RepoSyncPermissions(policy.RBACAdmin(), policy.CoreAdmin()))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	repoSyncKey := repoSyncID.ObjectKey
	gitSource := nt.SyncSources[repoSyncID]
	bookinfoRole := k8sobjects.RoleObject(core.Name("bookinfo-admin"))
	image, err := nt.BuildAndPushOCIImage(repoSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookinfoRole))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Create RepoSync with OCI image")
	repoSyncOCI := nt.RepoSyncObjectOCI(repoSyncKey, image.OCIImageID().WithoutDigest(), "", image.Digest)
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncOCI))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the RepoSync to sync from OCI"))
	nt.Must(nt.WatchForAllSyncs())

	err = nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, repoSyncID.Namespace,
		testwatcher.WatchPredicates(
			checkRepoSyncPreSyncAnnotations(),
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
			testpredicates.MissingAnnotation(metadata.SourceURLAnnotationKey),
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
	gitSource := nt.SyncSources[rootSyncID]
	bookinfoRole := k8sobjects.RoleObject(core.Name("bookinfo-admin"))
	image, err := nt.BuildAndPushOCIImage(rootSyncKey, registryproviders.ImageInputObjects(nt.Scheme, bookinfoRole))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Create RootSync with OCI image")
	rootSyncOCI := nt.RootSyncObjectOCI(rootSyncKey.Name, image.OCIImageID().WithoutDigest(), "", image.Digest)
	nt.Must(nt.KubeClient.Apply(rootSyncOCI))
	nt.Must(nt.WatchForAllSyncs())

	err = nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(
			checkRootSyncPreSyncAnnotations(),
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
			testpredicates.MissingAnnotation(metadata.SourceURLAnnotationKey),
		))
	if err != nil {
		nt.T.Fatalf("Source annotations still exist when RootSync is syncing from Git %v", err)
	}
}

func checkRootSyncPreSyncAnnotations() testpredicates.Predicate {
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
		commitAnnotation, ok := o.GetAnnotations()[metadata.SourceCommitAnnotationKey]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q", o.GetName(), metadata.SourceCommitAnnotationKey)
		}
		repoAnnotation, ok := o.GetAnnotations()[metadata.SourceURLAnnotationKey]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q", o.GetName(), metadata.SourceURLAnnotationKey)
		}
		if syncingCommit != commitAnnotation {
			return fmt.Errorf("object %s has commit annotation %s, but is being synced with commit %s",
				o.GetName(),
				commitAnnotation,
				syncingCommit,
			)
		}
		if syncingImage != repoAnnotation {
			return fmt.Errorf("object %s has commit annotation %s, but is being synced with commit %s",
				o.GetName(),
				commitAnnotation,
				syncingCommit,
			)
		}
		return nil
	}
}

func checkRepoSyncPreSyncAnnotations() testpredicates.Predicate {
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
		commitAnnotation, ok := o.GetAnnotations()[metadata.SourceCommitAnnotationKey]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q", o.GetName(), metadata.SourceCommitAnnotationKey)
		}
		repoAnnotation, ok := o.GetAnnotations()[metadata.SourceURLAnnotationKey]
		if !ok {
			return fmt.Errorf("object %q does not have annotation %q", o.GetName(), metadata.SourceURLAnnotationKey)
		}
		if syncingCommit != commitAnnotation {
			return fmt.Errorf("object %s has commit annotation %s, but is being synced with commit %s",
				o.GetName(),
				commitAnnotation,
				syncingCommit,
			)
		}
		if syncingImage != repoAnnotation {
			return fmt.Errorf("object %s has commit annotation %s, but is being synced with commit %s",
				o.GetName(),
				commitAnnotation,
				syncingCommit,
			)
		}
		return nil
	}
}
