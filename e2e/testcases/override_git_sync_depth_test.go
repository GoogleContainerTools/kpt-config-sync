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
	"time"

	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
)

func TestOverrideGitSyncDepthV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI,
		ntopts.RepoSyncWithGitSource(backendNamespace, configsync.RepoSyncName))
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	key := controllers.GitSyncDepth
	rootReconcilerNN := types.NamespacedName{
		Name:      nomostest.DefaultRootReconcilerName,
		Namespace: configsync.ControllerNamespace,
	}
	nsReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(backendNamespace, configsync.RepoSyncName),
		Namespace: configsync.ControllerNamespace,
	}

	err := validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, controllers.SyncDepthNoRev)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, controllers.SyncDepthNoRev)
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := k8sobjects.RootSyncObjectV1Alpha1(configsync.RootSyncName)

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)

	// Override the git sync depth setting for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 5}}}`)
	err = validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, "5")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for ns-reconciler-backend
	var depth int64 = 33
	repoSyncBackend.Spec.SafeOverride().GitSyncDepth = &depth
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync git sync depth to 33"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "33")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for root-reconciler to 0
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 0}}}`)
	err = validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, "0")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)

	// Override the git sync depth setting for ns-reconciler-backend to 0
	depth = 0
	repoSyncBackend.Spec.SafeOverride().GitSyncDepth = &depth
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync git sync depth to 0"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "0")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = &v1alpha1.RepoSyncOverrideSpec{}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Clear `spec.override` from repoSyncBackend"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "1")
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestOverrideGitSyncDepthV1Beta1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI,
		ntopts.RepoSyncWithGitSource(backendNamespace, configsync.RepoSyncName))
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	key := controllers.GitSyncDepth
	rootReconcilerNN := types.NamespacedName{
		Name:      nomostest.DefaultRootReconcilerName,
		Namespace: configsync.ControllerNamespace,
	}
	nsReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(backendNamespace, configsync.RepoSyncName),
		Namespace: configsync.ControllerNamespace,
	}

	err := validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, controllers.SyncDepthNoRev)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, controllers.SyncDepthNoRev)
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)

	// Override the git sync depth setting for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 5}}}`)
	err = validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, "5")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for ns-reconciler-backend
	var depth int64 = 33
	repoSyncBackend.Spec.SafeOverride().GitSyncDepth = &depth
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync git sync depth to 33"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "33")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for root-reconciler to 0
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 0}}}`)
	err = validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, "0")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)

	// Override the git sync depth setting for ns-reconciler-backend to 0
	depth = 0
	repoSyncBackend.Spec.SafeOverride().GitSyncDepth = &depth
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync git sync depth to 0"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "0")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = &v1beta1.RepoSyncOverrideSpec{}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Clear `spec.override` from repoSyncBackend"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "1")
	if err != nil {
		nt.T.Fatal(err)
	}
}

func validateDeploymentContainerHasEnvVar(nt *nomostest.NT, nn types.NamespacedName, container, key, value string) error {
	return nt.Watcher.WatchObject(kinds.Deployment(), nn.Name, nn.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentHasEnvVar(container, key, value),
		},
		testwatcher.WatchTimeout(30*time.Second))
}
