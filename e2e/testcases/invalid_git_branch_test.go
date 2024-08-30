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

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/status"
)

func TestInvalidRootSyncBranchStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Update RootSync to invalid branch name
	nomostest.SetRootSyncGitBranch(nt, configsync.RootSyncName, "invalid-branch")

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := rootSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update RootSync to valid branch name
	nomostest.SetRootSyncGitBranch(nt, configsync.RootSyncName, gitproviders.MainBranch)

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	if err := nomostest.ValidateStandardMetrics(nt); err != nil {
		nt.T.Fatal(err)
	}
}

func TestInvalidRepoSyncBranchStatus(t *testing.T) {
	repoSyncID := core.RepoSyncID(configsync.RepoSyncName, namespaceRepo)
	nt := nomostest.New(t, nomostesting.SyncSource,
		ntopts.SyncWithGitSource(repoSyncID))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitReadWriteRepository(repoSyncID)

	repoSync := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncKey)
	repoSync.Spec.Branch = "invalid-branch"
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(namespaceRepo, repoSync.Name), repoSync))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update RepoSync to invalid branch name"))

	nt.WaitForRepoSyncSourceError(namespaceRepo, configsync.RepoSyncName, status.SourceErrorCode, "")

	err := nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
		// RepoSync already included in the default resource count and operations
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	repoSyncLabels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncKey)
	if err != nil {
		nt.T.Fatal(err)
	}
	// TODO: Fix commit to be UNKNOWN (b/361182373)
	commitHash := repoSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		// Source error prevents apply, so don't wait for a sync with the current commit.
		nomostest.ReconcilerErrorMetrics(nt, repoSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	repoSync.Spec.Branch = gitproviders.MainBranch
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(namespaceRepo, repoSync.Name), repoSync))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update RepoSync to valid branch name"))

	// Ensure RepoSync's active branch is checked out, so the correct commit is used for validation.
	nt.Must(repoSyncGitRepo.CheckoutBranch(gitproviders.MainBranch))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
		// RepoSync already included in the default resource count and operations
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync:        repoSyncKey,
		ObjectCount: 0, // no additional managed objects
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestSyncFailureAfterSuccessfulSyncs(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	nt.T.Cleanup(func() {
		nt.T.Log("Resetting all RootSync branches to main")
		nt.Must(rootSyncGitRepo.CheckoutBranch(gitproviders.MainBranch))
		nomostest.SetRootSyncGitBranch(nt, configsync.RootSyncName, gitproviders.MainBranch)
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}
	})

	// Add audit namespace.
	auditNS := "audit"
	// The test will delete the branch later, but the main branch can't be deleted
	// on some Git providers (e.g. Bitbucket), so using a develop branch.
	devBranch := "develop"
	nt.Must(rootSyncGitRepo.CreateBranch(devBranch))
	nt.Must(rootSyncGitRepo.CheckoutBranch(devBranch))
	nt.Must(rootSyncGitRepo.Add(
		fmt.Sprintf("acme/namespaces/%s/ns.yaml", auditNS),
		k8sobjects.NamespaceObject(auditNS)))
	nt.Must(rootSyncGitRepo.CommitAndPushBranch("add namespace to acme directory", devBranch))

	// Update RootSync to sync from the dev branch
	nomostest.SetRootSyncGitBranch(nt, configsync.RootSyncName, devBranch)
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Validate namespace 'acme' created.
	err := nt.Validate(auditNS, "", k8sobjects.NamespaceObject(auditNS))
	if err != nil {
		nt.T.Error(err)
	}

	// Make the sync fail by invalidating the source repo.
	nt.Must(rootSyncGitRepo.RenameBranch(devBranch, "invalid-branch"))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")

	// Change the remote branch name back to the original name.
	nt.Must(rootSyncGitRepo.RenameBranch("invalid-branch", devBranch))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}
