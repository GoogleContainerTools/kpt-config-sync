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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e/nomostest"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core/k8sobjects"
)

func TestMultipleRemoteBranchesOutOfSync(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	if err := nt.KubeClient.Get(configsync.RootSyncName, configmanagement.ControllerNamespace, rs); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Create an extra remote tracking branch")
	nt.Must(rootSyncGitRepo.Push("HEAD:upstream/main"))

	nt.T.Logf("Update the remote main branch by adding a test namespace")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/hello/ns.yaml", k8sobjects.NamespaceObject("hello")))
	nt.Must(rootSyncGitRepo.CommitAndPush("add Namespace"))

	nt.T.Logf("Verify git-sync can pull the latest commit with the default branch and revision")
	// WatchForAllSyncs validates RootSync's lastSyncedCommit is updated to the
	// local HEAD with the DefaultRootSha1Fn function.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate("hello", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Remove the test namespace to make sure git-sync can fetch newer commit")
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/hello/ns.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove Namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound("hello", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}
}
