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

	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/system"
	"kpt.dev/configsync/pkg/status"
)

func TestMissingRepoErrorWithHierarchicalFormat(t *testing.T) {
	nt := nomostest.New(t)

	nomostest.SetPolicyDir(nt, configsync.RootSyncName, "")

	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, system.MissingRepoErrorCode, "")
	} else {
		nt.WaitForRepoImportErrorCode(system.MissingRepoErrorCode)
	}
}

func TestPolicyDirUnset(t *testing.T) {
	nt := nomostest.New(t)
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
	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/cluster/admin-clusterrole.yaml", "./cluster")
	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/namespaces", ".")
	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/system", ".")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Initialize the root directory")
	nt.WaitForRepoSyncs()

	nomostest.SetPolicyDir(nt, configsync.RootSyncName, "")
	nt.WaitForRepoSyncs(nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "."}))
}

func TestInvalidPolicyDir(t *testing.T) {
	nt := nomostest.New(t)

	nt.T.Log("Break the policydir in the repo")
	nomostest.SetPolicyDir(nt, configsync.RootSyncName, "some-nonexistent-policydir")

	nt.T.Log("Expect an error to be present in status.source.errors")
	if nt.MultiRepo {
		nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.PathErrorCode, "")
	} else {
		nt.WaitForRepoSourceError(status.SourceErrorCode)
	}

	nt.T.Log("Fix the policydir in the repo")
	nomostest.SetPolicyDir(nt, configsync.RootSyncName, "acme")
	nt.T.Log("Expect repo to recover from the error in source message")
	nt.WaitForRepoSyncs()
}
