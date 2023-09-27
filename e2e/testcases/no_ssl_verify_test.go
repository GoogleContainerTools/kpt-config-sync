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
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestNoSSLVerifyV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	key := controllers.GitSSLNoVerify
	rootReconcilerNN := types.NamespacedName{
		Name:      nomostest.DefaultRootReconcilerName,
		Namespace: v1.NSConfigManagementSystem,
	}
	nsReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(backendNamespace, configsync.RepoSyncName),
		Namespace: v1.NSConfigManagementSystem,
	}

	// verify the reconciler deployments don't have the key yet
	err := validateDeploymentContainerMissingEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}
	err = validateDeploymentContainerMissingEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := fake.RootSyncObjectV1Alpha1(configsync.RootSyncName)

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)

	// Set noSSLVerify to true for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": true}}}`)
	err = validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, "true")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to true for ns-reconciler-backend
	repoSyncBackend.Spec.NoSSLVerify = true
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to true"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "true")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to false for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": false}}}`)
	err = validateDeploymentContainerMissingEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to false from repoSyncBackend
	repoSyncBackend.Spec.NoSSLVerify = false
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to false"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerMissingEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestNoSSLVerifyV1Beta1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.OverrideAPI,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	key := controllers.GitSSLNoVerify
	rootReconcilerNN := types.NamespacedName{
		Name:      nomostest.DefaultRootReconcilerName,
		Namespace: v1.NSConfigManagementSystem,
	}
	nsReconcilerNN := types.NamespacedName{
		Name:      core.NsReconcilerName(backendNamespace, configsync.RepoSyncName),
		Namespace: v1.NSConfigManagementSystem,
	}

	// verify the reconciler deployments don't have the key yet
	err := validateDeploymentContainerMissingEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}
	err = validateDeploymentContainerMissingEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)

	// Set noSSLVerify to true for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": true}}}`)
	err = validateDeploymentContainerHasEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key, "true")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to true for ns-reconciler-backend
	repoSyncBackend.Spec.NoSSLVerify = true
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to true"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerHasEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key, "true")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to false for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": false}}}`)
	err = validateDeploymentContainerMissingEnvVar(nt, rootReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to false from repoSyncBackend
	repoSyncBackend.Spec.NoSSLVerify = false
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to false"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = validateDeploymentContainerMissingEnvVar(nt, nsReconcilerNN,
		reconcilermanager.GitSync, key)
	if err != nil {
		nt.T.Fatal(err)
	}
}

func validateDeploymentContainerMissingEnvVar(nt *nomostest.NT, nn types.NamespacedName, container, key string) error {
	return nt.Watcher.WatchObject(kinds.Deployment(), nn.Name, nn.Namespace,
		[]testpredicates.Predicate{
			testpredicates.DeploymentMissingEnvVar(container, key),
		},
		testwatcher.WatchTimeout(30*time.Second))
}
