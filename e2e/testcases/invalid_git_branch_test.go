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
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestInvalidRootSyncBranchStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)

	// Update RootSync to invalid branch name
	nomostest.SetGitBranch(nt, configsync.RootSyncName, "invalid-branch")

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update RootSync to valid branch name
	nomostest.SetGitBranch(nt, configsync.RootSyncName, gitproviders.MainBranch)

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	if err := nomostest.ValidateStandardMetrics(nt); err != nil {
		nt.T.Fatal(err)
	}
}

func TestInvalidRepoSyncBranchStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.NamespaceRepo(namespaceRepo, configsync.RepoSyncName))
	repoSyncNN := nomostest.RepoSyncNN(namespaceRepo, configsync.RepoSyncName)
	repoSync := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncNN)
	repoSync.Spec.Branch = "invalid-branch"
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(namespaceRepo, repoSync.Name), repoSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to invalid branch name"))

	nt.WaitForRepoSyncSourceError(namespaceRepo, configsync.RepoSyncName, status.SourceErrorCode, "")

	err := nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
		// RepoSync already included in the default resource count and operations
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	repoSyncLabels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.NonRootRepos[repoSyncNN].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		// Source error prevents apply, so don't wait for a sync with the current commit.
		nomostest.ReconcilerErrorMetrics(nt, repoSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	repoSync.Spec.Branch = gitproviders.MainBranch
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(namespaceRepo, repoSync.Name), repoSync))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to valid branch name"))

	// Ensure RepoSync's active branch is checked out, so the correct commit is used for validation.
	nt.Must(nt.NonRootRepos[repoSyncNN].CheckoutBranch(gitproviders.MainBranch))

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
		Sync:        repoSyncNN,
		ObjectCount: 0, // no additional managed objects
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestSyncFailureAfterSuccessfulSyncs(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)
	nt.T.Cleanup(func() {
		nt.T.Log("Resetting all RootSync branches to main")
		nt.Must(nt.RootRepos[configsync.RootSyncName].CheckoutBranch(gitproviders.MainBranch))
		nomostest.SetGitBranch(nt, configsync.RootSyncName, gitproviders.MainBranch)
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}
	})

	// Add audit namespace.
	auditNS := "audit"
	// The test will delete the branch later, but the main branch can't be deleted
	// on some Git providers (e.g. Bitbucket), so using a develop branch.
	devBranch := "develop"
	nt.Must(nt.RootRepos[configsync.RootSyncName].CreateBranch(devBranch))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CheckoutBranch(devBranch))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		fmt.Sprintf("acme/namespaces/%s/ns.yaml", auditNS),
		fake.NamespaceObject(auditNS)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPushBranch("add namespace to acme directory", devBranch))

	// Update RootSync to sync from the dev branch
	nomostest.SetGitBranch(nt, configsync.RootSyncName, devBranch)
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Validate namespace 'acme' created.
	err := nt.Validate(auditNS, "", fake.NamespaceObject(auditNS))
	if err != nil {
		nt.T.Error(err)
	}

	// Make the sync fail by invalidating the source repo.
	nt.Must(nt.RootRepos[configsync.RootSyncName].RenameBranch(devBranch, "invalid-branch"))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")

	// Change the remote branch name back to the original name.
	nt.Must(nt.RootRepos[configsync.RootSyncName].RenameBranch("invalid-branch", devBranch))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}
