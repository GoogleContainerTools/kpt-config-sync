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
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func privateCertSecretPatch(name string) string {
	return fmt.Sprintf(`{"spec": {"git": {"privateCertSecret": {"name": "%s"}}}}`, name)
}

func syncURLHTTPSPatch(url string) string {
	return fmt.Sprintf(`{"spec": {"git": {"repo": "%s", "auth": "none", "secretRef": {"name": ""}}}}`,
		url)
}

func syncURLSSHPatch(url string) string {
	return fmt.Sprintf(
		`{"spec": {"git": {"repo": "%s", "auth": "ssh", "secretRef": {"name": "%s"}}}}`,
		url, controllers.GitCredentialVolume)
}

func TestPrivateCertSecretV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.SkipNonLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()

	key := "GIT_SSL_CAINFO"
	privateCertSecret := "git-cert-pub"
	privateCertPath := "/etc/private-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := fake.RootSyncObjectV1Alpha1(configsync.RootSyncName)
	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)

	// Set RootSync SyncURL to use HTTPS
	rootSyncHTTPS := "https://test-git-server.config-management-system-test/git/config-management-system/root-sync"
	nt.MustMergePatch(rootSync, syncURLHTTPSPatch(rootSyncHTTPS))
	// RootSync should fail without privateCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", rootSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set privateCertSecret for RootSync
	nt.MustMergePatch(rootSync, privateCertSecretPatch(privateCertSecret))
	nt.WaitForRepoSyncs()
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, privateCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use HTTPS
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef.Name = ""
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use HTTPS")
	// RepoSync should fail without privateCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", repoSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set privateCertSecret for RepoSync
	repoSyncBackend.Spec.Git.PrivateCertSecret.Name = privateCertSecret
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync set privateCertSecret")
	nt.WaitForRepoSyncs()
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, privateCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset privateCertSecret for RootSync
	nt.MustMergePatch(rootSync, privateCertSecretPatch(""))
	// RootSync should fail without privateCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RootSync to use SSH again
	rootSyncSSHURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	nt.MustMergePatch(rootSync, syncURLSSHPatch(rootSyncSSHURL))
	nt.WaitForRepoSyncs()
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", rootSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset privateCertSecret for repoSyncBackend
	repoSyncBackend.Spec.Git.PrivateCertSecret.Name = ""
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync unset privateCertSecret")
	// RepoSync should fail without privateCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use SSH again
	repoSyncSSHURL := nt.GitProvider.SyncURL(nt.NonRootRepos[nn].RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef.Name = "ssh-key"
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use SSH")
	nt.WaitForRepoSyncs()
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", repoSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestPrivateCertSecretV1Beta1(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.SkipNonLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	nt.WaitForRepoSyncs()

	key := "GIT_SSL_CAINFO"
	privateCertSecret := "git-cert-pub"
	privateCertPath := "/etc/private-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)

	// Set RootSync SyncURL to use HTTPS
	rootSyncHTTPS := "https://test-git-server.config-management-system-test/git/config-management-system/root-sync"
	nt.MustMergePatch(rootSync, syncURLHTTPSPatch(rootSyncHTTPS))
	// RootSync should fail without privateCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", rootSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set privateCertSecret for RootSync
	nt.MustMergePatch(rootSync, privateCertSecretPatch(privateCertSecret))
	nt.WaitForRepoSyncs()
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, privateCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use HTTPS
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef.Name = ""
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use HTTPS")
	// RepoSync should fail without privateCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", repoSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set privateCertSecret for RepoSync
	repoSyncBackend.Spec.Git.PrivateCertSecret.Name = privateCertSecret
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync set privateCertSecret")
	nt.WaitForRepoSyncs()
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, key, privateCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset privateCertSecret for RootSync
	nt.MustMergePatch(rootSync, privateCertSecretPatch(""))
	// RootSync should fail without privateCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RootSync to use SSH again
	rootSyncSSHURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	nt.MustMergePatch(rootSync, syncURLSSHPatch(rootSyncSSHURL))
	nt.WaitForRepoSyncs()
	err = nt.Validate(nomostest.DefaultRootReconcilerName, v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", rootSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset privateCertSecret for repoSyncBackend
	repoSyncBackend.Spec.Git.PrivateCertSecret.Name = ""
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync unset privateCertSecret")
	// RepoSync should fail without privateCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use SSH again
	repoSyncSSHURL := nt.GitProvider.SyncURL(nt.NonRootRepos[nn].RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef.Name = "ssh-key"
	nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use SSH")
	nt.WaitForRepoSyncs()
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), v1.NSConfigManagementSystem, &appsv1.Deployment{}, nomostest.DeploymentHasEnvVar(reconcilermanager.GitSync, "GIT_SYNC_REPO", repoSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}
}
