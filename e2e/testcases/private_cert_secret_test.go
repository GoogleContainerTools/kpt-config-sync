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
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1alpha1"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/status"
)

func caCertSecretPatch(sourceType configsync.SourceType, name string) string {
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
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, backendNamespace)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitRepository(repoSyncID)
	rootSyncReconcilerName := nomostest.DefaultRootReconcilerName
	repoSyncReconcilerName := core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name)

	key := controllers.GitSSLCAInfo
	caCertSecret := nomostest.PublicCertSecretName(nomostest.GitSyncSource)
	caCertPath := "/etc/ca-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name), configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := k8sobjects.RootSyncObjectV1Alpha1(configsync.RootSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, repoSyncKey)

	// Set RootSync SyncURL to use HTTPS
	rootSyncHTTPS := "https://test-git-server.config-management-system-test/git/config-management-system/root-sync"
	nt.MustMergePatch(rootSync, syncURLHTTPSPatch(rootSyncHTTPS))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RootSync
	nt.MustMergePatch(rootSync, caCertSecretPatch(configsync.GitSource, caCertSecret))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use HTTPS
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef = &v1alpha1.SecretReference{}

	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync use HTTPS"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(repoSyncID.Namespace, repoSyncID.Name, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(repoSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RepoSync
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1alpha1.SecretReference{Name: caCertSecret}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync set caCertSecret"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(repoSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset caCertSecret for RootSync
	nt.MustMergePatch(rootSync, caCertSecretPatch(configsync.GitSource, ""))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RootSync to use SSH again
	rootSyncSSHURL := nt.GitProvider.SyncURL(rootSyncGitRepo.RemoteRepoName)
	nt.MustMergePatch(rootSync, syncURLSSHPatch(rootSyncSSHURL))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset caCertSecret for repoSyncBackend
	repoSyncBackend.Spec.Git.CACertSecretRef.Name = ""
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync unset caCertSecret"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(repoSyncID.Namespace, repoSyncID.Name, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(repoSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use SSH again
	repoSyncSSHURL := nt.GitProvider.SyncURL(repoSyncGitRepo.RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef = &v1alpha1.SecretReference{Name: "ssh-key"}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync use SSH"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(repoSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestCACertSecretRefV1Beta1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.RequireLocalGitProvider,
		ntopts.NamespaceRepo(backendNamespace, configsync.RepoSyncName))
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, backendNamespace)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitRepository(repoSyncID)
	rootSyncReconcilerName := nomostest.DefaultRootReconcilerName

	key := controllers.GitSSLCAInfo
	caCertSecret := nomostest.PublicCertSecretName(nomostest.GitSyncSource)
	caCertPath := "/etc/ca-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name), configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSync := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncKey)
	reconcilerName := core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name)

	// Set RootSync SyncURL to use HTTPS
	rootSyncHTTPS := "https://test-git-server.config-management-system-test/git/config-management-system/root-sync"
	nt.MustMergePatch(rootSync, syncURLHTTPSPatch(rootSyncHTTPS))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RootSync
	nt.MustMergePatch(rootSync, caCertSecretPatch(configsync.GitSource, caCertSecret))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use HTTPS
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync use HTTPS"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(repoSyncID.Namespace, repoSyncID.Name, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncHTTPS))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set caCertSecret for RepoSync
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync set caCertSecret"))
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
	nt.MustMergePatch(rootSync, caCertSecretPatch(configsync.GitSource, ""))
	// RootSync should fail without caCertSecret
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RootSync to use SSH again
	rootSyncSSHURL := nt.GitProvider.SyncURL(rootSyncGitRepo.RemoteRepoName)
	nt.MustMergePatch(rootSync, syncURLSSHPatch(rootSyncSSHURL))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, rootSyncSSHURL))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Unset caCertSecret for repoSyncBackend
	repoSyncBackend.Spec.Git.CACertSecretRef.Name = ""
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync unset caCertSecret"))
	// RepoSync should fail without caCertSecret
	nt.WaitForRepoSyncSourceError(repoSyncID.Namespace, repoSyncID.Name, status.SourceErrorCode, "server certificate verification failed")
	err = nt.Validate(reconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Set RepoSync to use SSH again
	repoSyncSSHURL := nt.GitProvider.SyncURL(repoSyncGitRepo.RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: "ssh-key"}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync use SSH"))
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
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, backendNamespace)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitRepository(repoSyncID)
	rootSyncReconcilerName := nomostest.DefaultRootReconcilerName
	repoSyncReconcilerName := core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name)

	key := controllers.GitSSLCAInfo
	caCertSecret := nomostest.PublicCertSecretName(nomostest.GitSyncSource)
	caCertPath := "/etc/ca-cert/cert"
	var err error

	// verify the deployment doesn't have the key yet
	err = nt.Validate(rootSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(repoSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentMissingEnvVar(reconcilermanager.GitSync, key))
	if err != nil {
		nt.T.Fatal(err)
	}

	repoSyncBackend := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncKey)

	// Set RepoSync to use HTTPS with caCertSecret
	repoSyncHTTPS := "https://test-git-server.config-management-system-test/git/backend/repo-sync"
	repoSyncBackend.Spec.Git.Repo = repoSyncHTTPS
	repoSyncBackend.Spec.Git.Auth = "none"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{}
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync use HTTPS with caCertSecret"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(repoSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, key, caCertPath))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Check that the namespace secret was upserted to c-m-s namespace
	cmsSecret := &corev1.Secret{}
	cmsSecretName := controllers.ReconcilerResourceName(repoSyncReconcilerName, caCertSecret)
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
	repoSyncSSHURL := nt.GitProvider.SyncURL(repoSyncGitRepo.RemoteRepoName)
	repoSyncBackend.Spec.Git.Repo = repoSyncSSHURL
	repoSyncBackend.Spec.Git.Auth = "ssh"
	repoSyncBackend.Spec.Git.SecretRef = &v1beta1.SecretReference{Name: "ssh-key"}
	repoSyncBackend.Spec.Git.CACertSecretRef = &v1beta1.SecretReference{}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), repoSyncBackend))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update backend RepoSync unset caCertSecret and use SSH"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(repoSyncReconcilerName, configsync.ControllerNamespace, &appsv1.Deployment{}, testpredicates.DeploymentHasEnvVar(reconcilermanager.GitSync, controllers.GitSyncRepo, repoSyncSSHURL))
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

	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	image, err := nt.BuildAndPushOCIImage(nomostest.RootSyncNN(configsync.RootSyncName), registryproviders.ImageInputObjects(nt.Scheme, k8sobjects.NamespaceObject("foo-ns")))
	if err != nil {
		nt.T.Fatal(err)
	}
	imageURL, err := image.RemoteAddressWithTag()
	if err != nil {
		nt.T.Fatalf("OCIImage.RemoteAddressWithTag: %v", err)
	}

	nt.T.Log("Set the RootSync to sync the OCI image without providing a CA cert")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "oci": {"image": "%s", "auth": "none"}, "git": null}}`,
		configsync.OciSource, imageURL))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RootSync")
	nt.MustMergePatch(rs, caCertSecretPatch(configsync.OciSource, caCertSecret))
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
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, backendNamespace)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncReconcilerName := core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name)

	caCertSecret := nomostest.PublicCertSecretName(nomostest.RegistrySyncSource)

	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, repoSyncKey)
	upsertedSecret := controllers.ReconcilerResourceName(repoSyncReconcilerName, caCertSecret)

	cm := k8sobjects.ConfigMapObject(core.Name("foo-cm"), core.Namespace(repoSyncID.Namespace))
	image, err := nt.BuildAndPushOCIImage(repoSyncKey, registryproviders.ImageInputObjects(nt.Scheme, cm))
	if err != nil {
		nt.T.Fatal(err)
	}
	imageURL, err := image.RemoteAddressWithTag()
	if err != nil {
		nt.T.Fatalf("OCIImage.RemoteAddressWithTag: %v", err)
	}

	nt.T.Log("Set the RepoSync to sync the OCI image without providing a CA cert")
	rs.Spec.SourceType = configsync.OciSource
	rs.Spec.Oci = &v1beta1.Oci{
		Image: imageURL,
		Auth:  "none",
	}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), rs))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the RepoSync to use OCI without providing CA cert"))

	nt.WaitForRepoSyncSourceError(repoSyncID.Namespace, repoSyncID.Name, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RepoSync")
	rs.Spec.Oci.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), rs))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the CA cert for the RepoSync"))
	err = nt.WatchForAllSyncs(
		nomostest.WithRepoSha1Func(imageDigestFuncByDigest(image.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			repoSyncKey: ".",
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
	rs.Spec.SourceType = configsync.GitSource
	nt.Must(rootSyncGitRepo.Add(
		nomostest.StructuredNSPath(repoSyncID.Namespace, repoSyncID.Name), rs))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the RepoSync to sync from Git"))

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

	rs := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	chart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmChartObjects(nt.Scheme, k8sobjects.NamespaceObject("foo-ns")))
	if err != nil {
		nt.T.Fatal(err)
	}
	chartRepoURL, err := chart.Provider.RepositoryRemoteURL()
	if err != nil {
		nt.T.Fatalf("HelmProvider.RepositoryRemoteURL: %v", err)
	}

	nt.T.Log("Set the RootSync to sync the Helm package without providing a CA cert")
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"sourceType": "%s", "helm": {"repo": "%s", "chart": "%s", "version": "%s", "auth": "none", "period": "15s"}, "git": null}}`,
		configsync.HelmSource, chartRepoURL, chart.Name, chart.Version))
	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RootSync")
	nt.MustMergePatch(rs, caCertSecretPatch(configsync.HelmSource, caCertSecret))
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
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)

	caCertSecret := nomostest.PublicCertSecretName(nomostest.RegistrySyncSource)

	nn := nomostest.RepoSyncNN(backendNamespace, configsync.RepoSyncName)
	rs := nomostest.RepoSyncObjectV1Beta1FromNonRootRepo(nt, nn)
	upsertedSecret := controllers.ReconcilerResourceName(
		core.NsReconcilerName(nn.Namespace, nn.Name), caCertSecret)

	cm := k8sobjects.ConfigMapObject(core.Name("foo-cm"), core.Namespace(nn.Namespace))
	chart, err := nt.BuildAndPushHelmPackage(nn, registryproviders.HelmChartObjects(nt.Scheme, cm))
	if err != nil {
		nt.T.Fatal(err)
	}
	chartRepoURL, err := chart.Provider.RepositoryRemoteURL()
	if err != nil {
		nt.T.Fatalf("HelmProvider.RepositoryRemoteURL: %v", err)
	}

	nt.T.Log("Set the RepoSync to sync the Helm package without providing a CA cert")
	rs.Spec.SourceType = configsync.HelmSource
	rs.Spec.Helm = &v1beta1.HelmRepoSync{
		HelmBase: v1beta1.HelmBase{
			Repo:    chartRepoURL,
			Chart:   chart.Name,
			Version: chart.Version,
			Auth:    "none",
			Period:  metav1.Duration{Duration: 15 * time.Second},
		},
	}
	nt.Must(rootSyncGitRepo.Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the RepoSync to use Helm without providing CA cert"))

	nt.WaitForRepoSyncSourceError(nn.Namespace, nn.Name, status.SourceErrorCode, "tls: failed to verify certificate: x509: certificate signed by unknown authority")

	nt.T.Log("Add caCertSecretRef to RepoSync")
	rs.Spec.Helm.CACertSecretRef = &v1beta1.SecretReference{Name: caCertSecret}
	nt.Must(rootSyncGitRepo.Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the CA cert for the RepoSync"))
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
	rs.Spec.SourceType = configsync.GitSource
	nt.Must(rootSyncGitRepo.Add(
		nomostest.StructuredNSPath(nn.Namespace, nn.Name), rs))
	nt.Must(rootSyncGitRepo.CommitAndPush("Set the RepoSync to sync from Git"))

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
