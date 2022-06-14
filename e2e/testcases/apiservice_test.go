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
	"kpt.dev/configsync/pkg/api/configsync"
)

func TestCreateAPIServiceAndEndpointInTheSameCommit(t *testing.T) {
	nt := nomostest.New(t)
	nt.T.Log("Creating commit with APIService and Deployment")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/namespace-custom-metrics.yaml", "acme/namespaces/custom-metrics/namespace-custom-metrics.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/apiservice.yaml", "acme/cluster/apiservice.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add testing resources")
	nt.T.Log("Waiting for nomos to sync new APIService")
	nt.WaitForRepoSyncs()

	nt.T.Log("Removing APIService")
	nt.RootRepos[configsync.RootSyncName].Remove("acme/cluster/apiservice.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove API service")
	nt.WaitForRepoSyncs()
}

func TestImporterAndSyncerResilientToBadAPIService(t *testing.T) {
	nt := nomostest.New(t)
	nt.T.Log("Adding bad APIService")
	nt.MustKubectl("apply", "-f", "../testdata/apiservice/apiservice.yaml")
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "-f", "../testdata/apiservice/apiservice.yaml", "--ignore-not-found")
	})

	nt.T.Log("Creating commit with Deployment")
	nt.RootRepos[configsync.RootSyncName].Copy("../testdata/apiservice/namespace-resilient.yaml", "acme/namespaces/resilient/namespace-resilient.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add testing resources")
	nt.T.Log("Waiting for nomos to stabilize")
	nt.WaitForRepoSyncs()
}
