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
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func caCertSecretPatch(name string) string {
	return fmt.Sprintf(`{"spec": {"git": {"caCertSecretRef": {"name": "%s"}}}}`, name)
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

func secretDataPatch(key, value string) string {
	value64 := base64.StdEncoding.EncodeToString([]byte(value))
	return fmt.Sprintf(`{"data": {"%s": "%s"}}`, key, value64)
}

func secretDataDeletePatch(key string) string {
	return fmt.Sprintf(`{"data": {"%s": null}}`, key)
}

func TestCACertSecretRefV1Alpha1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.SkipNonLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))

	key := controllers.GitSSLCAInfo
	caCertSecret := "git-cert-pub"
	caCertPath := "/etc/ca-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := fake.RootSyncObjectV1Alpha1(configsync.RootSyncName)
	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)
	reconcilerName := core.NsReconcilerName(backendNamespace, configsync.RepoSyncName)

	// Set RootSync SyncURL to use HTTPS
	rootSyncHTTPS := "https://test-git-server.config-management-system-test/git/config-management-system/root-sync"
	nt.MustMergePatch(rootSync, syncURLHTTPSPatch(rootSyncHTTPS))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RootSync
	nt.MustMergePatch(rootSync, caCertSecretPatch(caCertSecret))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use HTTPS
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef = &v1alpha1.SecretReference{}

	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use HTTPS"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RepoSync
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1alpha1.SecretReference{Name: caCertSecret}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync set caCertSecret"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset caCertSecret for RootSync
	nt.MustMergePatch(rootSync, caCertSecretPatch(""))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RootSync to use SSH again
	rootSyncSSHURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	nt.MustMergePatch(rootSync, syncURLSSHPatch(rootSyncSSHURL))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset caCertSecret for repoSyncBackend
	repoSyncBackend.Spec.Git.CACertSecretRef.Name = ""
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync unset caCertSecret"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use SSH again
	repoSyncSSHURL := nt.GitProvider.SyncURL(nt.NonRootRepos[nn].RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef = &v1alpha1.SecretReference{Name: "ssh-key"}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use SSH"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestCACertSecretRefV1Beta1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.SkipNonLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))

	key := controllers.GitSSLCAInfo
	caCertSecret := "git-cert-pub"
	caCertPath := "/etc/ca-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)
	reconcilerName := core.NsReconcilerName(backendNamespace, configsync.RepoSyncName)

	// Set RootSync SyncURL to use HTTPS
	rootSyncHTTPS := "https://test-git-server.config-management-system-test/git/config-management-system/root-sync"
	nt.MustMergePatch(rootSync, syncURLHTTPSPatch(rootSyncHTTPS))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RootSync
	nt.MustMergePatch(rootSync, caCertSecretPatch(caCertSecret))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use HTTPS
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use HTTPS"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RepoSync
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync set caCertSecret"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Check that the namespace secret was upserted to c-m-s namespace
	err = nt.Validate(controllers.ReconcilerResourceName(reconcilerName, caCertSecret), configsync.ControllerNamespace, &corev1.Secret{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset caCertSecret for RootSync
	nt.MustMergePatch(rootSync, caCertSecretPatch(""))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RootSync to use SSH again
	rootSyncSSHURL := nt.GitProvider.SyncURL(nt.RootRepos[configsync.RootSyncName].RemoteRepoName)
	nt.MustMergePatch(rootSync, syncURLSSHPatch(rootSyncSSHURL))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset caCertSecret for repoSyncBackend
	repoSyncBackend.Spec.Git.CACertSecretRef.Name = ""
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync unset caCertSecret"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(backendNamespace, configsync.RepoSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use SSH again
	repoSyncSSHURL := nt.GitProvider.SyncURL(nt.NonRootRepos[nn].RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: "ssh-key"}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use SSH"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestCACertSecretWatch(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.SkipNonLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))

	key := controllers.GitSSLCAInfo
	caCertSecret := "git-cert-pub"
	caCertPath := "/etc/ca-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(nomostest.DefaultRootReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(core.NsReconcilerName(backendNamespace, configsync.RepoSyncName), configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)
	reconcilerName := core.NsReconcilerName(backendNamespace, configsync.RepoSyncName)

	// Set RepoSync to use HTTPS with caCertSecret
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{}
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync use HTTPS with caCertSecret"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Check that the namespace secret was upserted to c-m-s namespace
	cmsSecret := &corev1.Secret{}
	cmsSecretName := controllers.ReconcilerResourceName(reconcilerName, caCertSecret)
	err = nt.Validate(cmsSecretName, configsync.ControllerNamespace, cmsSecret)
	if err != nil {
		nt.T.Fatal(err)
	}
	// Modify the secret in c-m-s namespace
	nt.MustMergePatch(cmsSecret, secretDataPatch("foo", "bar"))
	// Check that watch triggered resync of the c-m-s secret
	require.NoError(nt.T,
		nt.Watcher.WatchObject(kinds.Secret(), cmsSecretName, configsync.ControllerNamespace, []testpredicates.Predicate{
			testpredicates.SecretMissingKey("foo"),
		}))
	// Modify the secret in RepoSync namespace
	rsSecret := &corev1.Secret{}
	err = nt.Validate(caCertSecret, backendNamespace, rsSecret)
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.MustMergePatch(rsSecret, secretDataPatch("baz", "bat"))
	// Check that the watch triggered upsert to c-m-s secret
	require.NoError(nt.T,
		nt.Watcher.WatchObject(kinds.Secret(), cmsSecretName, configsync.ControllerNamespace, []testpredicates.Predicate{
			testpredicates.SecretHasKey("baz", "bat"),
		}))
	// Unset caCertSecret for repoSyncBackend and use SSH
	repoSyncSSHURL := nt.GitProvider.SyncURL(nt.NonRootRepos[nn].RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: "ssh-key"}
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(nomostest.StructuredNSPath(backendNamespace, configsync.RepoSyncName), repoSyncBackend))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update backend RepoSync unset caCertSecret and use SSH"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}
}
