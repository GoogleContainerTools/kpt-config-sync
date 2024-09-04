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

	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

const (
	githubAppRepoBranch              = "main"
	githubAppRepoNamespace           = "github-app-test"
	githubAppRepoConfigMap           = "test-cm"
	githubAppRepoNamespacedConfigDir = "namespaced"
)

// Manual provisioning steps required for the githubapp tests to be runnable:
// - Private Github repository created containing expected resources
// - Github app created
// - Github configured with read access to the private repository
// - GitHub app configuration stored on local filesystem or GCP Secret Manager
//
// Expected format of repository (see above constants for expected NN values):
//
//	.
//	├── LICENSE
//	├── namespaced
//	│         └── cm.yaml
//	├── namespace-github-app-test.yaml
//	└── README.md
//
// TODO: implement a gitprovider for github and replace these one-off tests
func TestGithubAppRootSync(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncNN := rootSyncID.ObjectKey
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.GitHubAppTest)
	githubApp, err := gitproviders.FetchGithubAppConfiguration()
	if err != nil {
		nt.T.Fatal(err)
	}
	// Verify the repo is private
	nt.T.Log("The repository should not be publicly accessible")
	rs := k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
	rs.Spec.Git = &v1beta1.Git{
		Repo: githubApp.Repo(),
		Auth: configsync.AuthNone,
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.WaitForRootSyncSourceError(rootSyncNN.Name, status.SourceErrorCode,
		`Authentication failed`)
	// Verify reconciler-manager checks existence of Secret
	nt.T.Log("The reconciler-manager should report a validation error for missing Secret")
	emptySecretNN := types.NamespacedName{Name: "empty-secret", Namespace: rootSyncNN.Namespace}
	rs = k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
	rs.Spec = v1beta1.RootSyncSpec{
		SourceFormat: configsync.SourceFormatUnstructured,
		Git: &v1beta1.Git{
			Repo:      githubApp.Repo(),
			Branch:    githubAppRepoBranch,
			Auth:      configsync.AuthGithubApp,
			SecretRef: &v1beta1.SecretReference{Name: emptySecretNN.Name},
		},
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.WaitForRootSyncStalledError(rs.Name, "Validation",
		`Secret empty-secret not found: create one to allow client authentication`)
	// Verify reconciler-manager checks validity of Secret data fields
	nt.T.Log("The reconciler-manager should report a validation error for invalid Secret")
	emptySecret := k8sobjects.SecretObject(emptySecretNN.Name, core.Namespace(rootSyncNN.Namespace))
	nt.T.Cleanup(func() {
		nt.Must(nt.KubeClient.Delete(emptySecret))
	})
	nt.Must(nt.KubeClient.Create(emptySecret))
	nt.WaitForRootSyncStalledError(rs.Name, "Validation",
		`git secretType was set as "githubapp" but github-app-private-key key is not present in empty-secret secret`)
	// Verify reconciler can sync using client ID
	nt.T.Log("The reconciler should successfully sync using githubapp auth with client ID")
	secretWithClientIDNN := types.NamespacedName{Name: "githubapp-creds-clientid", Namespace: rootSyncNN.Namespace}
	nt.Must(createGithubSecretWithClientID(nt, githubApp, secretWithClientIDNN))
	rs = k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
	rs.Spec = v1beta1.RootSyncSpec{
		SourceFormat: configsync.SourceFormatUnstructured,
		Git: &v1beta1.Git{
			Repo:      githubApp.Repo(),
			Branch:    githubAppRepoBranch,
			Auth:      configsync.AuthGithubApp,
			SecretRef: &v1beta1.SecretReference{Name: secretWithClientIDNN.Name},
		},
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), githubAppRepoNamespace, "", nil))
	nt.Must(nt.Watcher.WatchObject(kinds.ConfigMap(), githubAppRepoConfigMap, githubAppRepoNamespace, nil))
	// This hack exists because we don't have a way to authenticate to the repo from
	// the test framework. The branch is not expected to change, so just trust the
	// SHA reported on the RootSync.
	// TODO: replace this if we implement a gitprovider interface
	rs = k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
	nt.Must(nt.KubeClient.Get(rs.Name, rs.Namespace, rs))
	nomostest.SetExpectedSyncSource(nt, rootSyncID, &syncsource.GitSyncSource{
		Repository: gitproviders.ReadOnlyRepository{
			URL: githubApp.Repo(),
		},
		Branch:         githubAppRepoBranch,
		SourceFormat:   configsync.SourceFormatUnstructured,
		ExpectedCommit: rs.Status.Source.Commit,
	})
	nt.Must(nt.WatchForAllSyncs())
	// Verify reconciler can sync using application ID
	nt.T.Log("The reconciler should successfully sync using githubapp auth with application ID")
	nt.Must(nt.KubeClient.Delete(rs)) // Recreate RootSync to ensure status is current
	nt.Must(nt.Watcher.WatchForNotFound(kinds.RootSyncV1Beta1(), rootSyncNN.Name, rootSyncNN.Namespace))
	secretWithAppIDNN := types.NamespacedName{Name: "githubapp-creds-appid", Namespace: rootSyncNN.Namespace}
	nt.Must(createGithubSecretWithApplicationID(nt, githubApp, secretWithAppIDNN))
	rs = k8sobjects.RootSyncObjectV1Beta1(rootSyncNN.Name)
	rs.Spec = v1beta1.RootSyncSpec{
		SourceFormat: configsync.SourceFormatUnstructured,
		Git: &v1beta1.Git{
			Repo:      githubApp.Repo(),
			Branch:    githubAppRepoBranch,
			Auth:      configsync.AuthGithubApp,
			SecretRef: &v1beta1.SecretReference{Name: secretWithAppIDNN.Name},
		},
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.Must(nt.WatchForAllSyncs())
}

func TestGithubAppRepoSync(t *testing.T) {
	repoSyncNN := nomostest.RepoSyncNN(githubAppRepoNamespace, "rs-test")
	repoSyncID := core.RepoSyncID(repoSyncNN.Name, repoSyncNN.Namespace)
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.GitHubAppTest,
		ntopts.WithDelegatedControl,                    // DelegatedControl to simplify updating RepoSync
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // RepoSync manages ConfigMap
		ntopts.SyncWithGitSource(repoSyncID))
	githubApp, err := gitproviders.FetchGithubAppConfiguration()
	if err != nil {
		nt.T.Fatal(err)
	}
	// Verify the repo is private
	nt.T.Log("The repository should not be publicly accessible")
	rs := k8sobjects.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name)
	rs.Spec.Git = &v1beta1.Git{
		Repo: githubApp.Repo(),
		Auth: configsync.AuthNone,
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.WaitForRepoSyncSourceError(repoSyncNN.Namespace, repoSyncNN.Name, status.SourceErrorCode,
		`Authentication failed`)
	// Verify reconciler-manager checks existence of Secret
	nt.T.Log("The reconciler-manager should report a validation error for missing Secret")
	emptySecretNN := types.NamespacedName{
		Name:      "empty-secret",
		Namespace: repoSyncNN.Namespace,
	}
	rs = k8sobjects.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name)
	rs.Spec = v1beta1.RepoSyncSpec{
		SourceFormat: configsync.SourceFormatUnstructured,
		Git: &v1beta1.Git{
			Repo:      githubApp.Repo(),
			Branch:    githubAppRepoBranch,
			Auth:      configsync.AuthGithubApp,
			SecretRef: &v1beta1.SecretReference{Name: emptySecretNN.Name},
		},
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation",
		`Secret empty-secret not found: create one to allow client authentication`)
	// Verify reconciler-manager checks validity of Secret data fields
	nt.T.Log("The reconciler-manager should report a validation error for invalid Secret")
	emptySecret := k8sobjects.SecretObject(emptySecretNN.Name, core.Namespace(repoSyncNN.Namespace))
	nt.T.Cleanup(func() {
		nt.Must(nt.KubeClient.Delete(emptySecret))
	})
	nt.Must(nt.KubeClient.Create(emptySecret))
	nt.WaitForRepoSyncStalledError(rs.Namespace, rs.Name, "Validation",
		`git secretType was set as "githubapp" but github-app-private-key key is not present in empty-secret secret`)
	// Verify reconciler can sync using client ID
	nt.T.Log("The reconciler should successfully sync using githubapp auth with client ID")
	secretWithClientIDNN := types.NamespacedName{
		Name:      "githubapp-creds-clientid",
		Namespace: repoSyncNN.Namespace,
	}
	nt.Must(createGithubSecretWithClientID(nt, githubApp, secretWithClientIDNN))
	rs = k8sobjects.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name)
	rs.Spec = v1beta1.RepoSyncSpec{
		SourceFormat: configsync.SourceFormatUnstructured,
		Git: &v1beta1.Git{
			Repo:      githubApp.Repo(),
			Branch:    githubAppRepoBranch,
			Dir:       githubAppRepoNamespacedConfigDir,
			Auth:      configsync.AuthGithubApp,
			SecretRef: &v1beta1.SecretReference{Name: secretWithClientIDNN.Name},
		},
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.Must(nt.Watcher.WatchObject(kinds.ConfigMap(), githubAppRepoConfigMap, githubAppRepoNamespace, nil))
	// This hack exists because we don't have a way to authenticate to the repo from
	// the test framework. The branch is not expected to change, so just trust the
	// SHA reported on the RepoSync.
	// TODO: replace this if we implement a gitprovider interface
	rs = k8sobjects.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name)
	nt.Must(nt.KubeClient.Get(rs.Name, rs.Namespace, rs))
	nomostest.SetExpectedSyncSource(nt, repoSyncID, &syncsource.GitSyncSource{
		Repository: gitproviders.ReadOnlyRepository{
			URL: githubApp.Repo(),
		},
		Branch:            githubAppRepoBranch,
		Revision:          "",
		SourceFormat:      configsync.SourceFormatUnstructured,
		Directory:         "",
		ExpectedDirectory: githubAppRepoNamespacedConfigDir,
		ExpectedCommit:    rs.Status.Source.Commit,
	})
	nt.Must(nt.WatchForAllSyncs())
	// Verify reconciler can sync using application ID
	nt.T.Log("The reconciler should successfully sync using githubapp auth with application ID")
	nt.Must(nt.KubeClient.Delete(rs)) // Recreate RepoSync to ensure status is current
	nt.Must(nt.Watcher.WatchForNotFound(kinds.RepoSyncV1Beta1(), repoSyncNN.Name, repoSyncNN.Namespace))
	secretWithAppIDNN := types.NamespacedName{
		Name:      "githubapp-creds-appid",
		Namespace: repoSyncNN.Namespace,
	}
	nt.Must(createGithubSecretWithApplicationID(nt, githubApp, secretWithAppIDNN))
	rs = k8sobjects.RepoSyncObjectV1Beta1(repoSyncNN.Namespace, repoSyncNN.Name)
	rs.Spec = v1beta1.RepoSyncSpec{
		SourceFormat: configsync.SourceFormatUnstructured,
		Git: &v1beta1.Git{
			Repo:      githubApp.Repo(),
			Branch:    githubAppRepoBranch,
			Dir:       githubAppRepoNamespacedConfigDir,
			Auth:      configsync.AuthGithubApp,
			SecretRef: &v1beta1.SecretReference{Name: secretWithAppIDNN.Name},
		},
	}
	nt.Must(nt.KubeClient.Apply(rs))
	nt.Must(nt.WatchForAllSyncs())
}

func createGithubSecretWithClientID(nt *nomostest.NT, githubApp *gitproviders.GithubAppConfiguration, nn types.NamespacedName) error {
	secret, err := githubApp.SecretWithClientID(nn)
	if err != nil {
		return err
	}
	nt.T.Cleanup(func() {
		nt.Must(nt.KubeClient.Delete(k8sobjects.SecretObject(nn.Name, core.Namespace(nn.Namespace))))
	})
	return nt.KubeClient.Create(secret)
}

func createGithubSecretWithApplicationID(nt *nomostest.NT, githubApp *gitproviders.GithubAppConfiguration, nn types.NamespacedName) error {
	secret, err := githubApp.SecretWithApplicationID(nn)
	if err != nil {
		return err
	}
	nt.T.Cleanup(func() {
		nt.Must(nt.KubeClient.Delete(k8sobjects.SecretObject(nn.Name, core.Namespace(nn.Namespace))))
	})
	return nt.KubeClient.Create(secret)
}
