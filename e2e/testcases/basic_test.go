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
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
)

const (
	yamlDir = "../testdata"
)

func TestNamespaceGarbageCollection(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/accounting-namespace.yaml", yamlDir), "acme/namespaces/accounting/namespace.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add accounting namespace"))
	nt.Must(nt.WatchForAllSyncs())

	if err := nt.Validate("accounting", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/accounting/namespace.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove accounting namespace"))
	nt.Must(nt.WatchForAllSyncs())

	if err := nt.ValidateNotFound("accounting", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal("Namespace still exist when it should have been garbage collected")
	}
}

func TestNamespacePolicyspaceConversion(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add dir namespace"))
	nt.Must(nt.WatchForAllSyncs())

	if err := nt.Validate("dir", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/dir/namespace.yaml"))
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/subdir-namespace.yaml", yamlDir), "acme/namespaces/dir/subdir/namespace.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove dir namespace, add subdir namespace"))
	nt.Must(nt.WatchForAllSyncs())

	if err := nt.Validate("subdir", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("dir", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal("Namespace still exist when it should have been converted")
	}
}

func TestSyncDeploymentAndReplicaSet(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Test the ability to fix a mistake: overlapping replicaset and deployment.
	// Readiness behavior is undefined for this race condition.
	// One or both of the Deployment and ReplicaSet may become unhealthy.
	// But regardless, the user should be able to correct the situation.
	nt.T.Log("Add a replicaset")
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml"))
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/replicaset-helloworld.yaml", yamlDir), "acme/namespaces/dir/replicaset.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add replicaset"))

	// This sync may block until reconcile timeout is reached,
	// because ReplicaSet or Deployment may never reconcile.
	// So this wait timeout must be longer than the reconcile timeout (5m).
	nt.Must(nt.WatchForAllSyncs())
	nt.T.Log("check that the replicaset was created")
	if err := nt.Validate("hello-world", "dir", &appsv1.ReplicaSet{}, testpredicates.HasLabel("app", "hello-world")); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add a corresponding deployment")
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/namespaces/dir/deployment.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add corresponding deployment"))
	nt.Must(nt.WatchForAllSyncs(nomostest.SkipAllResourceGroupChecks()))

	nt.T.Log("check that the deployment was created")
	if err := nt.Validate("hello-world", "dir", &appsv1.Deployment{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Remove the deployment")
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/dir/deployment.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove deployment"))
	// This sync may block until reconcile timeout is reached,
	// because the ReplicaSet is re-applied before deleting the Deployment.
	// So this wait timeout must be longer than the reconcile timeout (5m).
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("check that the deployment was removed and replicaset remains")
	if err := nt.ValidateNotFound("hello-world", "dir", &appsv1.Deployment{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate("hello-world", "dir", &appsv1.ReplicaSet{}, testpredicates.HasLabel("app", "hello-world")); err != nil {
		nt.T.Fatal(err)
	}
}

func TestRolebindingsUpdated(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.Must(rootSyncGitRepo.Copy("../../examples/acme/namespaces/eng/backend/namespace.yaml", "acme/namespaces/eng/backend/namespace.yaml"))
	nt.Must(rootSyncGitRepo.Copy("../../examples/acme/namespaces/eng/backend/bob-rolebinding.yaml", "acme/namespaces/eng/backend/br.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add bob rolebinding"))
	nt.Must(nt.WatchForAllSyncs())
	if err := nt.Validate("bob-rolebinding", "backend", &rbacv1.RoleBinding{}, testpredicates.RoleBindingHasName("acme-admin")); err != nil {
		nt.T.Fatal("bob-rolebinding not found")
	}

	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/robert-rolebinding.yaml", yamlDir), "acme/namespaces/eng/backend/br.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Replace bob with robert rolebinding"))
	nt.Must(nt.WatchForAllSyncs())

	if err := nt.ValidateNotFound("bob-rolebinding", "backend", &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal("bob-rolebinding is not deleted")
	}

	if err := nt.Validate("robert-rolebinding", "backend", &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal("robert-rolebinding not found")
	}
}

func manageNamespace(nt *nomostest.NT, namespace string) {
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	nt.T.Log("Add an unmanaged resource into the namespace as a control")
	nt.T.Log("We should never modify this resource")
	_, err := nt.Shell.Kubectl("apply", "-f", fmt.Sprintf("%s/reserved_namespaces/unmanaged-service.%s.yaml", yamlDir, namespace))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Cleanup(func() {
		svcObj := k8sobjects.ServiceObject(core.Name("some-other-service"), core.Namespace(namespace))
		if err := nomostest.DeleteObjectsAndWait(nt, svcObj); err != nil {
			nt.T.Fatal(err)
		}
	})

	nt.T.Log("Add resource to manage")
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/reserved_namespaces/service.yaml", yamlDir), fmt.Sprintf("acme/namespaces/%s/service.yaml", namespace)))
	nt.T.Log("Start managing the namespace")
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/reserved_namespaces/namespace.%s.yaml", yamlDir, namespace), fmt.Sprintf("acme/namespaces/%s/namespace.yaml", namespace)))
	nt.Must(rootSyncGitRepo.CommitAndPush("Start managing the namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Validate managed service appears on the cluster")
	if err := nt.Validate("some-service", namespace, &corev1.Service{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Remove the namespace directory from the repo")
	nt.Must(rootSyncGitRepo.Remove(fmt.Sprintf("acme/namespaces/%s", namespace)))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove the namespace from the managed set of namespaces"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Validate managed service disappears from the cluster")
	if err := nt.ValidateNotFound("some-service", namespace, &corev1.Service{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Ensure that the unmanaged service remained")
	if err := nt.Validate("some-other-service", namespace, &corev1.Service{}); err != nil {
		nt.T.Fatal(err)
	}
}

func unmanageNamespace(nt *nomostest.NT, namespace string) {
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)
	nt.T.Log("stop managing the system namespace")
	nt.Must(rootSyncGitRepo.Copy(fmt.Sprintf("%s/reserved_namespaces/unmanaged-namespace.%s.yaml", yamlDir, namespace), fmt.Sprintf("acme/namespaces/%s/namespace.yaml", namespace)))
	nt.Must(rootSyncGitRepo.CommitAndPush("Stop managing the namespace"))
	nt.Must(nt.WatchForAllSyncs())
}

func TestNamespaceDefaultCanBeManaged(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	manageNamespace(nt, "default")
	unmanageNamespace(nt, "default")
}

func TestNamespaceGatekeeperSystemCanBeManaged(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	nt.MustKubectl("apply", "-f", fmt.Sprintf("%s/reserved_namespaces/namespace.gatekeeper-system.yaml", yamlDir))
	t.Cleanup(func() { nt.MustKubectl("delete", "ns", "gatekeeper-system", "--ignore-not-found") })
	manageNamespace(nt, "gatekeeper-system")
}

func TestNamespaceKubeSystemCanBeManaged(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.SkipAutopilotCluster)
	manageNamespace(nt, "kube-system")
	unmanageNamespace(nt, "kube-system")
}

func TestNamespaceKubePublicCanBeManaged(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	manageNamespace(nt, "kube-public")
	unmanageNamespace(nt, "kube-public")
}

func TestMaxRootSyncNameLength(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))

	repo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// The name max length is 63 - len("config-management-system_") due to the inventory-id label.
	// Thus, the max length name for a RootSync is 38 characters.

	// Test scenario for RootSync that exceeds the max name length by one
	rootSyncTooLongName := strings.Repeat("n", 39)
	rootSyncTooLong := nomostest.RootSyncObjectV1Beta1FromOtherRootRepo(
		nt, rootSyncTooLongName, nomostest.DefaultRootSyncID.Name)
	// Share the repository but set a separate sync dir for the new RootSync
	defaultSyncDir := rootSyncTooLong.Spec.Dir
	rootSyncTooLong.Spec.Git.Dir = rootSyncTooLongName
	nt.Must(repo.Add(fmt.Sprintf("%s/rs1.yaml", defaultSyncDir), rootSyncTooLong))
	nt.Must(repo.Add(
		fmt.Sprintf("%s/ns.yaml", rootSyncTooLongName),
		k8sobjects.NamespaceObject(rootSyncTooLongName)))
	nt.Must(repo.CommitAndPush("create RootSync with too long name"))
	nt.Must(nt.Watcher.WatchForRootSyncStalledError(rootSyncTooLongName, "Validation",
		"maximum RootSync name length is 38, but found 39"))

	// Test scenario for RootSync with exactly the max name length
	rootSyncMaxLengthName := strings.Repeat("y", 38)
	rootSyncMaxLength := nomostest.RootSyncObjectV1Beta1FromOtherRootRepo(
		nt, rootSyncMaxLengthName, nomostest.DefaultRootSyncID.Name)
	rootSyncMaxLength.Spec.Git.Dir = rootSyncMaxLengthName
	nt.Must(repo.Add(fmt.Sprintf("%s/rs2.yaml", defaultSyncDir), rootSyncMaxLength))
	nt.Must(repo.Add(
		fmt.Sprintf("%s/ns.yaml", rootSyncMaxLengthName),
		k8sobjects.NamespaceObject(rootSyncMaxLengthName)))
	nt.Must(repo.CommitAndPush("create RootSync with max length name"))
	nt.Must(nt.WatchForSync(kinds.RootSyncV1Beta1(), rootSyncMaxLengthName, configsync.ControllerNamespace,
		&syncsource.GitSyncSource{
			ExpectedCommit:    repo.MustHash(t),
			ExpectedDirectory: rootSyncMaxLengthName,
		}))
}

func TestMaxRepoSyncNameLength(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.RepoSyncPermissions(policy.CoreAdmin()))

	repo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	rootSyncDir := gitproviders.DefaultSyncDir
	// The ns-reconciler deployment label is constructed as "ns-reconciler-<ns>-<name>-<len(name)>"
	// Thus, the max length name + namespace for a RepoSync is 45 characters.
	repoSyncNS := strings.Repeat("x", 10)
	nt.Must(repo.Add(fmt.Sprintf("%s/ns.yaml", rootSyncDir), k8sobjects.NamespaceObject(repoSyncNS)))
	nt.Must(repo.CommitAndPush("Create RepoSync namespace"))
	nt.Must(nt.WatchForAllSyncs())
	nt.Must(nomostest.CreateNamespaceSecrets(nt, repoSyncNS))

	// Test scenario for RepoSync that exceeds the max name length by one
	repoSyncTooLongNN := types.NamespacedName{
		Name:      strings.Repeat("n", 36),
		Namespace: repoSyncNS,
	}
	repoSyncTooLong := nomostest.RepoSyncObjectV1Beta1FromOtherRootRepo(
		nt, repoSyncTooLongNN, nomostest.DefaultRootSyncID.Name)
	// Share the repository but set a separate sync dir for the new RepoSync
	syncDir := repoSyncTooLongNN.Name
	repoSyncTooLong.Spec.Git.Dir = syncDir
	nt.Must(repo.Add(fmt.Sprintf("%s/rs1.yaml", rootSyncDir), repoSyncTooLong))
	nt.Must(repo.Add(fmt.Sprintf("%s/rb-1.yaml", rootSyncDir),
		nomostest.RepoSyncRoleBinding(repoSyncTooLongNN)))
	nt.Must(repo.Add(fmt.Sprintf("%s/cr.yaml", rootSyncDir), nt.RepoSyncClusterRole()))
	nt.Must(repo.Add(
		fmt.Sprintf("%s/cm.yaml", syncDir),
		k8sobjects.ConfigMapObject(
			core.Name(repoSyncTooLongNN.Name), core.Namespace(repoSyncTooLongNN.Namespace))))
	nt.Must(repo.CommitAndPush("create RepoSync with too long NN"))
	nt.Must(nt.Watcher.WatchForRepoSyncStalledError(repoSyncTooLongNN.Namespace, repoSyncTooLongNN.Name,
		"Validation", "maximum combined length of RepoSync name and namespace is 45, but found 46"))

	// Test scenario for RepoSync with exactly the max name length
	repoSyncMaxLengthNN := types.NamespacedName{
		Name:      strings.Repeat("y", 35),
		Namespace: repoSyncNS,
	}
	repoSyncMaxLength := nomostest.RepoSyncObjectV1Beta1FromOtherRootRepo(
		nt, repoSyncMaxLengthNN, nomostest.DefaultRootSyncID.Name)
	syncDir = repoSyncMaxLengthNN.Name
	repoSyncMaxLength.Spec.Git.Dir = syncDir
	nt.Must(repo.Add(fmt.Sprintf("%s/rs2.yaml", rootSyncDir), repoSyncMaxLength))
	nt.Must(repo.Add(fmt.Sprintf("%s/rb-2.yaml", rootSyncDir),
		nomostest.RepoSyncRoleBinding(repoSyncMaxLengthNN)))
	nt.Must(repo.Add(
		fmt.Sprintf("%s/cm.yaml", syncDir),
		k8sobjects.ConfigMapObject(
			core.Name(repoSyncMaxLengthNN.Name), core.Namespace(repoSyncMaxLengthNN.Namespace))))
	nt.Must(repo.CommitAndPush("create RepoSync with max length NN"))
	nt.Must(nt.WatchForSync(kinds.RepoSyncV1Beta1(), repoSyncMaxLengthNN.Name, repoSyncMaxLengthNN.Namespace,
		&syncsource.GitSyncSource{
			ExpectedCommit:    repo.MustHash(t),
			ExpectedDirectory: syncDir,
		}))
}
