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

	appsv1 "k8s.io/api/apps/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconciler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestOverrideGitSyncDepthV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()

	key := "GIT_SYNC_DEPTH"

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.GitSyncDepthOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, controllers.SyncDepthNoRev))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, controllers.SyncDepthNoRev))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repo, exist := nt.NonRootRepos[nn]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}
	rootSync := fake.RootSyncObjectV1Alpha1(configsync.RootSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1(nn.Namespace, nn.Name, nt.GitProvider.SyncURL(repo.RemoteRepoName))

	// Override the git sync depth setting for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 5}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "5"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for ns-reconciler-backend
	var depth int64 = 33
	repoSyncBackend.Spec.Override.GitSyncDepth = &depth
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync git sync depth to 33")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "33"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for root-reconciler to 0
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 0}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "0"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateGitSyncDepthOverrideCount(2)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)

	// Override the git sync depth setting for ns-reconciler-backend to 0
	depth = 0
	repoSyncBackend.Spec.Override.GitSyncDepth = &depth
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync git sync depth to 0")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "0"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = v1alpha1.OverrideSpec{}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncBackend")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "1"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.GitSyncDepthOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestOverrideGitSyncDepthV1Beta1(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()
	key := "GIT_SYNC_DEPTH"

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.GitSyncDepthOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, controllers.SyncDepthNoRev))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, controllers.SyncDepthNoRev))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repo, exist := nt.NonRootRepos[nn]
	if !exist {
		nt.T.Fatal("nonexistent repo")
	}
	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1(nn.Namespace, nn.Name, nt.GitProvider.SyncURL(repo.RemoteRepoName))

	// Override the git sync depth setting for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 5}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "5"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for ns-reconciler-backend
	var depth int64 = 33
	repoSyncBackend.Spec.Override.GitSyncDepth = &depth
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync git sync depth to 33")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "33"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Override the git sync depth setting for root-reconciler to 0
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"gitSyncDepth": 0}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "0"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateGitSyncDepthOverrideCount(2)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Clear `spec.override` from the RootSync
	nt.MustMergePatch(rootSync, `{"spec": {"override": null}}`)

	// Override the git sync depth setting for ns-reconciler-backend to 0
	depth = 0
	repoSyncBackend.Spec.Override.GitSyncDepth = &depth
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync git sync depth to 0")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "0"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Clear `spec.override` from repoSyncBackend
	repoSyncBackend.Spec.Override = v1beta1.OverrideSpec{}
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Clear `spec.override` from repoSyncBackend")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "1"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.GitSyncDepthOverrideCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}
}
