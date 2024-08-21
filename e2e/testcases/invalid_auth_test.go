// Copyright 2023 Google LLC
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
	"kpt.dev/configsync/e2e/nomostest/metrics"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
)

func TestInvalidAuth(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	// Update RootSync to sync from a GitHub repo with an ssh key.
	// The ssh key only works for test-git-server, not GitHub, so it will get a permission error.
	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, fmt.Sprintf(`{
		"spec": {
			"git": {
				"repo": "%s",
				"auth": "%s",
				"secretRef": {
					"name": "%s"
				}
			}
		}
	}`, "git@github.com:config-sync-examples/not-exist", configsync.AuthSSH, controllers.GitCredentialVolume))

	if err := nomostest.SetupFakeSSHCreds(nt, rs.Kind, nomostest.RootSyncNN(rs.Name), configsync.AuthSSH, controllers.GitCredentialVolume); err != nil {
		nt.T.Fatal(err)
	}
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "Permission denied")

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	// TODO: Fix commit to be UNKNOWN (b/361182373)
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	commitHash := rootSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
}
