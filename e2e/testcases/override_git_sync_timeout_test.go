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

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestOverrideRootSyncGitSyncTimeout(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.Unstructured)

	rootSyncName := nomostest.RootSyncNN(configsync.RootSyncName)
	rootReconcilerName := core.RootReconcilerObjectKey(rootSyncName.Name)
	rootSyncV1 := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)

	// apply override to one container and validate the others are unaffected
	nt.MustMergePatch(rootSyncV1, `{"spec": {"override": {"gitSyncOverride": {"gitSyncTimeout": 500}}}}`)

	err := nt.Watcher.WatchObject(kinds.Deployment(),
		rootReconcilerName.Name, rootReconcilerName.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "--timeout=500"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestOverrideRepoSyncGitSyncTimeout(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI, ntopts.Unstructured, ntopts.NamespaceRepo(frontendNamespace, configsync.RepoSyncName))
	frontendReconcilerNN := core.NsReconcilerObjectKey(frontendNamespace, configsync.RepoSyncName)
	frontendNN := nomostest.RepoSyncNN(frontendNamespace, configsync.RepoSyncName)
	repoSyncFrontend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, frontendNN)

	// Override the log level of the reconciler container of ns-reconciler-frontend
	repoSyncFrontend.Spec.Override = &v1beta1.RepoSyncOverrideSpec{
		OverrideSpec: v1beta1.OverrideSpec{
			GitSyncOverride: v1beta1.GitSyncOverride{
				GitSyncTimeout: 500,
			},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(frontendNamespace, configsync.RepoSyncName), repoSyncFrontend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update log level of frontend Reposync"))

	// validate override and make sure other containers are unaffected
	err := nt.Watcher.WatchObject(kinds.Deployment(),
		frontendReconcilerNN.Name, frontendReconcilerNN.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentContainerArgsContains(reconcilermanager.GitSync, "--timeout=500"),
		},
	)
	if err != nil {
		nt.T.Fatal(err)
	}

}
