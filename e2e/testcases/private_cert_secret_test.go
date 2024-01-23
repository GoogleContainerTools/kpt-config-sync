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
	"time"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
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

func caCertSecretPatch(sourceType v1beta1.SourceType, name string) string {
	return fmt.Sprintf(`{"spec": {"%s": {"caCertSecretRef": {"name": "%s"}}}}`, sourceType, name)
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
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))

	key := controllers.GitSSLCAInfo
	caCertSecret := nomostest.PublicCertSecretName(nomostest.GitSyncSource)
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
	nt.MustMergePatch(rootSync, caCertSecretPatch(v1beta1.GitSource, caCertSecret))
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
	nt.MustMergePatch(rootSync, caCertSecretPatch(v1beta1.GitSource, ""))
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
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))

	key := controllers.GitSSLCAInfo
	caCertSecret := nomostest.PublicCertSecretName(nomostest.GitSyncSource)
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
	nt.MustMergePatch(rootSync, caCertSecretPatch(v1beta1.GitSource, caCertSecret))
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
	nt.MustMergePatch(rootSync, caCertSecretPatch(v1beta1.GitSource, ""))
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
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))

	key := controllers.GitSSLCAInfo
	caCertSecret := nomostest.PublicCertSecretName(nomostest.GitSyncSource)
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

// TestOCICACertSecretRefRootRepo can run only run on KinD clusters.
// It tests RootSyncs can pull from OCI images using a CA certificate.
func TestOCICACertSecretRefRootRepo(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireLocalOCIProvider)

	caCertSecret := nomostest.PublicCertSecretName(nomostest.RegistrySyncSource)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("ns.yaml", fake.NamespaceObject("foo-ns")))

	image, err := nt.BuildAndPushOCIImage(nt.RootRepos[configsync.RootSyncName])
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set the RootSync to sync the OCI image without providing a CA cert")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"image": "%s", "auth": "none"}, "git": null}}`,
		v1beta1.OciSource, image.FloatingBranchTag()))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RootSync")
	nt.MustMergePatch(rs, caCertSecretPatch(v1beta1.OciSource, caCertSecret))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByDigest(image.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: ".",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestOCICACertSecretRefNamespaceRepo can run only run on KinD clusters.
// It tests RepoSyncs can pull from OCI images using a CA certificate.
func TestOCICACertSecretRefNamespaceRepo(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireLocalOCIProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.RepoSyncPermissions(policy.CoreAdmin()))

	caCertSecret := nomostest.PublicCertSecretName(nomostest.RegistrySyncSource)

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)
	upsertedSecret := controllers.ReconcilerResourceName(
		core.NsReconcilerName(nn.Namespace, nn.Name), caCertSecret)

	cm := fake.ConfigMapObject(core.Name("foo-cm"), core.Namespace(nn.Namespace))

	nt.Must(nt.NonRootRepos[nn].Add("ns.yaml", cm))

	image, err := nt.BuildAndPushOCIImage(nt.NonRootRepos[nn])
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set the RepoSync to sync the OCI image without providing a CA cert")
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Oci = &v1beta1.Oci{
		Image: image.FloatingBranchTag(),
		Auth:  "none",
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Set the RepoSync to use OCI without providing CA cert"))

	nt.WaitForRepoSyncSourceError(nn.Namespace, nn.Name, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RepoSync")
	rs.Spec.Oci.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Set the CA cert for the RepoSync"))
	err = nt.WatchForAllSyncs(
		nomostest.WithRepoSha1Func(imageDigestFuncByDigest(image.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nn: ".",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the ConfigMap was created")
	if err := nt.Validate(cm.Name, cm.Namespace, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the upserted Secret was created")
	if err := nt.Validate(upsertedSecret, configsync.ControllerNamespace, &corev1.Secret{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set the RepoSync to sync from git")
	rs.Spec.SourceType = string(v1beta1.GitSource)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Set the RepoSync to sync from Git"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the ConfigMap was pruned")
	if err := nt.ValidateNotFound(cm.Name, cm.Namespace, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the upserted Secret was garbage collected")
	if err := nt.ValidateNotFound(upsertedSecret, configsync.ControllerNamespace, &corev1.Secret{}); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmCACertSecretRefRootRepo can run only run on KinD clusters.
// It tests RootSyncs can pull from OCI images using a CA certificate.
func TestHelmCACertSecretRefRootRepo(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireLocalHelmProvider)

	caCertSecret := nomostest.PublicCertSecretName(nomostest.RegistrySyncSource)

	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("templates/ns.yaml", fake.NamespaceObject("foo-ns")))

	chart, err := nt.BuildAndPushHelmPackage(nt.RootRepos[configsync.RootSyncName])
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set the RootSync to sync the Helm package without providing a CA cert")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"repo": "%s", "chart": "%s", "version": "%s", "auth": "none", "period": "15s"}, "git": null}}`,
		v1beta1.HelmSource, nt.HelmProvider.SyncURL(chart.Name), chart.Name, chart.Version))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RootSync")
	nt.MustMergePatch(rs, caCertSecretPatch(v1beta1.HelmSource, caCertSecret))
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: chart.Name,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the Namespace was created")
	if err := nt.Validate("foo-ns", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}
}

// TestHelmCACertSecretRefNamespaceRepo can run only run on KinD clusters.
// It tests RepoSyncs can pull from OCI images using a CA certificate.
func TestHelmCACertSecretRefNamespaceRepo(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.Unstructured,
		ntopts.RequireLocalHelmProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName),
		ntopts.RepoSyncPermissions(policy.CoreAdmin()))

	caCertSecret := nomostest.PublicCertSecretName(nomostest.RegistrySyncSource)

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)
	upsertedSecret := controllers.ReconcilerResourceName(
		core.NsReconcilerName(nn.Namespace, nn.Name), caCertSecret)

	cm := fake.ConfigMapObject(core.Name("foo-cm"), core.Namespace(nn.Namespace))

	nt.Must(nt.NonRootRepos[nn].Add("templates/ns.yaml", cm))

	chart, err := nt.BuildAndPushHelmPackage(nt.NonRootRepos[nn])
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set the RepoSync to sync the Helm package without providing a CA cert")
	rs.Spec.SourceType = string(v1beta1.HelmSource)
	rs.Spec.Helm = &v1beta1.HelmRepoSync{
		HelmBase: v1beta1.HelmBase{
			Repo:    nt.HelmProvider.SyncURL(chart.Name),
			Chart:   chart.Name,
			Version: chart.Version,
			Auth:    "none",
			Period:  metav1.Duration{Duration: 15 * time.Second},
		},
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Set the RepoSync to use Helm without providing CA cert"))

	nt.WaitForRepoSyncSourceError(nn.Namespace, nn.Name, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RepoSync")
	rs.Spec.Helm.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Set the CA cert for the RepoSync"))
	err = nt.WatchForAllSyncs(
		nomostest.WithRepoSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nn: chart.Name,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the ConfigMap was created")
	if err := nt.Validate(cm.Name, cm.Namespace, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the upserted Secret was created")
	if err := nt.Validate(upsertedSecret, configsync.ControllerNamespace, &corev1.Secret{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Set the RepoSync to sync from git")
	rs.Spec.SourceType = string(v1beta1.GitSource)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Set the RepoSync to sync from Git"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the ConfigMap was pruned")
	if err := nt.ValidateNotFound(cm.Name, cm.Namespace, &corev1.ConfigMap{}); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Verify the upserted Secret was garbage collected")
	if err := nt.ValidateNotFound(upsertedSecret, configsync.ControllerNamespace, &corev1.Secret{}); err != nil {
		nt.T.Fatal(err)
	}
}
