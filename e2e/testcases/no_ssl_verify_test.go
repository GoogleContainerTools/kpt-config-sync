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
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconciler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
)

func TestNoSSLVerifyV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()

	key := "GIT_SSL_NO_VERIFY"

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.NoSSLVerifyCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// verify the deployment doesn't have the key yet
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
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

	// Set noSSLVerify to true for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": true}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "true"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to true for ns-reconciler-backend
	repoSyncBackend.Spec.NoSSLVerify = true
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to true")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "true"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateNoSSLVerifyCount(2)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Set noSSLVerify to false for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": false}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to false from repoSyncBackend
	repoSyncBackend.Spec.NoSSLVerify = false
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to false")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.NoSSLVerifyCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestNoSSLVerifyV1Beta1(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()

	key := "GIT_SSL_NO_VERIFY"

	err := nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.NoSSLVerifyCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// verify the deployment doesn't have the key yet
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
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

	// Set noSSLVerify to true for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": true}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "true"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to true for ns-reconciler-backend
	repoSyncBackend.Spec.NoSSLVerify = true
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to true")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, "true"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateNoSSLVerifyCount(2)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Set noSSLVerify to false for root-reconciler
	nt.MustMergePatch(rootSync, `{"spec": {"git": {"noSSLVerify": false}}}`)
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set noSSLVerify to false from repoSyncBackend
	repoSyncBackend.Spec.NoSSLVerify = false
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync NoSSLVerify to false")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate(reconciler.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		return nt.ValidateMetricNotFound(ocmetrics.NoSSLVerifyCountView.Name)
	})
	if err != nil {
		nt.T.Error(err)
	}
}
