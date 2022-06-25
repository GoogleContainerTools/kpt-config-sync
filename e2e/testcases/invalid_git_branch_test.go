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
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestInvalidRootSyncBranchStatus(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo)

	// Update RootSync to invalid branch name
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"branch": "invalid-branch"}}}`)

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")

	err := nt.ValidateMetrics(nomostest.SyncMetricsToReconcilerSourceError(nomostest.DefaultRootReconcilerName), func() error {
		// Validate reconciler error metric is emitted.
		return nt.ValidateReconcilerErrors(nomostest.DefaultRootReconcilerName, "source")
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Update RootSync to valid branch name
	rs = fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"branch": "main"}}}`)

	nt.WaitForRepoSyncs()

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestInvalidRepoSyncBranchStatus(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.NamespaceRepo(namespaceRepo, configsync.RepoSyncName))

	nn := nomostest.RepoSyncNN(namespaceRepo, configsync.RepoSyncName)
	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)
	rs.Spec.Branch = "invalid-branch"
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(namespaceRepo, rs.Name), rs)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update RepoSync to invalid branch name")

	nt.WaitForRepoSyncSourceError(namespaceRepo, configsync.RepoSyncName, status.SourceErrorCode, "")

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		// Validate reconciler error metric is emitted.
		return nt.ValidateReconcilerErrors(nomostest.DefaultRootReconcilerName, "source")
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
	nt := nomostest.New(t, ntopts.SkipMultiRepo)

	resetGitBranch(nt, "invalid-branch")

	nt.WaitForRepoSourceError(status.SourceErrorCode)

	resetGitBranch(nt, "main")
	nt.WaitForRepoSourceErrorClear()
}

func TestSyncFailureAfterSuccessfulSyncs(t *testing.T) {
	nt := nomostest.New(t)

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
	if nt.MultiRepo {
		rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"branch": "%s"}}}`, devBranch))
	} else {
		resetGitBranch(nt, devBranch)
	}
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
	// Reset RootSync because the cleanup stage will check if RootSync is synced from the main branch.
	if nt.MultiRepo {
		rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
		nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"git": {"branch": "%s"}}}`, nomostest.MainBranch))
	} else {
		resetGitBranch(nt, nomostest.MainBranch)
	}
}

// resetGitBranch updates GIT_SYNC_BRANCH in the config map and restart the reconcilers.
func resetGitBranch(nt *nomostest.NT, branch string) {
	nt.T.Logf("Change the GIT_SYNC_BRANCH name to %q", branch)
	cm := &corev1.ConfigMap{}
	err := nt.Get("git-sync", configmanagement.ControllerNamespace, cm)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.MustMergePatch(cm, fmt.Sprintf(`{"data":{"GIT_SYNC_BRANCH":"%s"}}`, branch))

	if nt.MultiRepo {
		nomostest.DeletePodByLabel(nt, "app", "reconciler-manager", true)
	} else {
		nomostest.DeletePodByLabel(nt, "app", "git-importer", false)
		nomostest.DeletePodByLabel(nt, "app", "monitor", false)
	}
}
