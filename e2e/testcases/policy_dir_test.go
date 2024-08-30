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
	"testing"

	"kpt.dev/configsync/e2e/nomostest"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
)

func TestMissingRepoErrorWithHierarchicalFormat(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)

	nomostest.SetRootSyncGitDir(nt, configsync.RootSyncName, "")

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, "")
}

func TestPolicyDirUnset(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.SyncSource)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(rootSyncID)
	// There are 6 cluster-scoped objects under `../../examples/acme/cluster`.
	//
	// Copying the whole `../../examples/acme/cluster` dir would cause the Config Sync mono-repo mode CI job to fail,
	// which runs on a shared cluster and calls resetSyncedRepos at the end of every e2e test.
	//
	// The reason for the failure is that if there are more than 1 cluster-scoped objects in a Git repo,
	// Config Sync mono-repo mode does not allow removing all these cluster-scoped objects in a single commit,
	// and generates a KNV 2006 error (as shown in http://b/210525686#comment3 and http://b/210525686#comment5).
	//
	// Therefore, we only copy `../../examples/acme/cluster/admin-clusterrole.yaml` here.
	nt.Must(rootSyncGitRepo.Copy("../../examples/acme/cluster/admin-clusterrole.yaml", "./cluster"))
	nt.Must(rootSyncGitRepo.Copy("../../examples/acme/namespaces", "."))
	nt.Must(rootSyncGitRepo.Copy("../../examples/acme/system", "."))
	nt.Must(rootSyncGitRepo.CommitAndPush("Initialize the root directory"))
	nt.Must(nt.WatchForAllSyncs())

	nomostest.SetRootSyncGitDir(nt, rootSyncID.Name, "")
	nomostest.SetExpectedSyncPath(nt, rootSyncID, controllers.DefaultSyncDir)
	nt.Must(nt.WatchForAllSyncs())
}

func TestInvalidPolicyDir(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource)

	nt.T.Log("Break the policydir in the repo")
	nomostest.SetRootSyncGitDir(nt, configsync.RootSyncName, "some-nonexistent-policydir")

	nt.T.Log("Expect an error to be present in status.source.errors")
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "")

	nt.T.Log("Fix the policydir in the repo")
	nomostest.SetRootSyncGitDir(nt, configsync.RootSyncName, "acme")
	nt.T.Log("Expect repo to recover from the error in source message")
	nt.Must(nt.WatchForAllSyncs())
}
