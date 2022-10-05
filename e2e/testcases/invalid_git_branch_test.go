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
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestInvalidRootSyncBranchStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.SkipMonoRepo)

	// Update RootSync to invalid branch name
	nomostest.SetGitBranch(nt, configsync.RootSyncName, "invalid-branch")

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")

	err := nt.ValidateMetrics(nomostest.SyncMetricsToReconcilerSourceError(nt, nomostest.DefaultRootReconcilerName), func() error {
		// Validate reconciler error metric is emitted.
		return nt.ValidateReconcilerErrors(nomostest.DefaultRootReconcilerName, 1, 0)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Update RootSync to valid branch name
	nomostest.SetGitBranch(nt, configsync.RootSyncName, nomostest.MainBranch)

	nt.WaitForRepoSyncs()

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestInvalidRepoSyncBranchStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.SkipMonoRepo, ntopts.NamespaceRepo(namespaceRepo, configsync.RepoSyncName))

	nn := nomostest.RepoSyncNN(namespaceRepo, configsync.RepoSyncName)
	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)
	rs.Spec.Branch = "invalid-branch"
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(namespaceRepo, rs.Name), rs)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to invalid branch name")

	nt.WaitForRepoSyncSourceError(namespaceRepo, configsync.RepoSyncName, status.SourceErrorCode, "")

	nsReconcilerName := core.NsReconcilerName(nn.Namespace, nn.Name)
	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		// Validate reconciler error metric is emitted.
		err := nt.ValidateReconcilerErrors(nomostest.DefaultRootReconcilerName, 0, 0)
		if err != nil {
			return err
		}
		return nt.ValidateReconcilerErrors(nsReconcilerName, 1, 0)
	})
	if err != nil {
		nt.T.Error(err)
	}

	rs.Spec.Branch = nomostest.MainBranch
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(namespaceRepo, rs.Name), rs)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to valid branch name")

	nt.WaitForRepoSyncs()

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestInvalidMonoRepoBranchStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.SkipMultiRepo)

	nomostest.SetGitBranch(nt, "unused", "invalid-branch")

	nt.WaitForRepoSourceError(status.SourceErrorCode)

	nomostest.SetGitBranch(nt, "unused", "main")
	nt.WaitForRepoSourceErrorClear()
}

func TestSyncFailureAfterSuccessfulSyncs(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)

	// Add audit namespace.
	auditNS := "audit"
	// The test will delete the branch later, but the main branch can't be deleted
	// on some Git providers (e.g. Bitbucket), so using a develop branch.
	devBranch := "develop"
	nt.RootRepos[configsync.RootSyncName].CreateBranch(devBranch)
	nt.RootRepos[configsync.RootSyncName].CheckoutBranch(devBranch)
	nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", auditNS),
		fake.NamespaceObject(auditNS))
	nt.RootRepos[configsync.RootSyncName].CommitAndPushBranch("add namespace to acme directory", devBranch)

	// Update RootSync to sync from the dev branch
	nomostest.SetGitBranch(nt, configsync.RootSyncName, devBranch)
	nt.WaitForRepoSyncs()

	// Validate namespace 'acme' created.
	err := nt.Validate(auditNS, "", fake.NamespaceObject(auditNS))
	if err != nil {
		nt.T.Error(err)
	}

	// Make the sync fail by invalidating the source repo.
	nt.RootRepos[configsync.RootSyncName].RenameBranch(devBranch, "invalid-branch")
	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")
	} else {
		nt.WaitForRepoSourceError(status.SourceErrorCode)
	}

	// Change the remote branch name back to the original name.
	nt.RootRepos[configsync.RootSyncName].RenameBranch("invalid-branch", devBranch)
	nt.WaitForRepoSyncs()
}
