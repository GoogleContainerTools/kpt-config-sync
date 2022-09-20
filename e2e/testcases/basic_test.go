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
	"time"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
)

const (
	yamlDir = "../testdata"
)

func TestNoDefaultFieldsInNamespaceConfig(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)

	nt.T.Log("Add a deployment")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding deployment")
	nt.WaitForRepoSyncs()

	nt.T.Log("Check that the deployment was created")
	_, err := nt.Kubectl("get", "deployment", "hello-world", "-n", "dir")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Check that specified field name is present")
	selector := ".spec.resources[0].versions[0].objects[0].metadata.name"
	name, err := nt.Kubectl("get", "namespaceconfig", "dir", "-o", fmt.Sprintf("jsonpath={%s}", selector))
	if err != nil {
		nt.T.Fatal(err)
	}
	if string(name) != "hello-world" {
		nt.T.Fatal("NamespaceConfig is missing Deployment hello-world")
	}

	nt.T.Log("Check that unspecified field creationTimestamp is not present")
	selector = ".spec.resources[0].versions[0].objects[0].metadata.creationTimestamp"
	ct, err := nt.Kubectl("get", "namespaceconfig", "dir", "-o", fmt.Sprintf("jsonpath={%s}", selector))
	if err != nil {
		nt.T.Fatal(err)
	}
	if len(ct) != 0 {
		nt.T.Fatal("NamespaceConfig has default field creationTimestamp which was not specified")
	}
}

func TestRecreateSyncDeletedByUser(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)

	nt.T.Log("Add a deployment to a directory")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a deployment to a directory")
	nt.WaitForRepoSyncs()

	nt.T.Log("Ensure that the system created a sync for the deployment")
	_, err := nt.Kubectl("get", "sync", "deployment.apps")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Force-delete the sync from the cluster")
	_, err = nomostest.Retry(60*time.Second, func() error {
		_, err := nt.Kubectl("delete", "sync", "deployment.apps")
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Ensure that the system re-created the force-deleted sync")
	_, err = nomostest.Retry(60*time.Second, func() error {
		_, err := nt.Kubectl("get", "sync", "deployment.apps")
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestDeletingClusterConfigRecoverable(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add dir namespace")
	nt.WaitForRepoSyncs()

	_, err := nt.Kubectl("get", "ns", "dir")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Forcefully delete clusterconfigs and verify recovery")
	_, err = nt.Kubectl("delete", "clusterconfig", "--all")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/cluster/admin-clusterrole.yaml", "acme/cluster/admin-clusterrole.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add admin clusterrole")
	nt.WaitForRepoSyncs()

	_, err = nt.Kubectl("get", "clusterconfig", "config-management-cluster-config")
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestNamespaceGarbageCollection(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/accounting-namespace.yaml", yamlDir), "acme/namespaces/accounting/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add accounting namespace")
	nt.WaitForRepoSyncs()
	_, err := nomostest.Retry(60*time.Second, func() error {
		_, err := nt.Kubectl("get", "ns", "accounting")
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/accounting/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove accounting namespace")
	nt.WaitForRepoSyncs()

	_, err = nt.Kubectl("get", "ns", "accounting")
	if err == nil {
		nt.T.Fatal("Namespace still exist when it should have been garbage collected")
	}
}

func TestNamespacePolicyspaceConversion(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add dir namespace")
	nt.WaitForRepoSyncs()

	_, err := nt.Kubectl("get", "ns", "dir")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/subdir-namespace.yaml", yamlDir), "acme/namespaces/dir/subdir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove dir namespace, add subdir namespace")
	nt.WaitForRepoSyncs()

	_, err = nomostest.Retry(60*time.Second, func() error {
		_, err := nt.Kubectl("get", "ns", "subdir")
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	_, err = nt.Kubectl("get", "ns", "dir")
	if err == nil {
		nt.T.Fatal("Namespace still exist when it should have been converted")
	}
}

func TestSyncDeploymentAndReplicaSet(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)

	// Test the ability to fix a mistake: overlapping replicaset and deployment.
	// Readiness behavior is undefined for this race condition.
	// One or both of the Deployment and ReplicaSet may become unhealthy.
	// But regardless, the user should be able to correct the situation.
	nt.T.Log("Add a replicaset")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/replicaset-helloworld.yaml", yamlDir), "acme/namespaces/dir/replicaset.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add replicaset")

	// This sync may block until reconcile timeout is reached,
	// because ReplicaSet or Deployment may never reconcile.
	// So this wait timeout must be longer than the reconcile timeout (5m).
	nt.WaitForRepoSyncs()
	nt.T.Log("check that the replicaset was created")
	_, err := nt.Kubectl("get", "replicaset", "-n", "dir", "-l", "app=hello-world")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add a corresponding deployment")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add corresponding deployment")
	nt.WaitForRepoSyncs()

	nt.T.Log("check that the deployment was created")
	_, err = nt.Kubectl("get", "deployment", "hello-world", "-n", "dir")
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Remove the deployment")
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove deployment")
	nt.WaitForRepoSyncs()

	nt.T.Log("check that the deployment was removed and replicaset remains")
	_, err = nt.Kubectl("get", "deployment", "hello-world", "-n", "dir")
	if err == nil {
		nt.T.Fatal(err)
	}
	_, err = nomostest.Retry(60*time.Second, func() error {
		_, err := nt.Kubectl("get", "replicaset", "-n", "dir", "-l", "app=hello-world")
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestRolebindingsUpdated(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)

	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/namespaces/eng/backend/namespace.yaml", "acme/namespaces/eng/backend/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/namespaces/eng/backend/bob-rolebinding.yaml", "acme/namespaces/eng/backend/br.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add bob rolebinding")
	nt.WaitForRepoSyncs()
	_, err := nt.Kubectl("get", "rolebinding", "-n", "backend", "bob-rolebinding")
	if err != nil {
		nt.T.Fatal("bob-rolebinding not found")
	}

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/robert-rolebinding.yaml", yamlDir), "acme/namespaces/eng/backend/br.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Replace bob with robert rolebinding")
	nt.WaitForRepoSyncs()

	_, err = nt.Kubectl("get", "rolebinding", "-n", "backend", "bob-rolebinding")
	if err == nil {
		nt.T.Fatal("bob-rolebinding is not deleted")
	}

	_, err = nt.Kubectl("get", "rolebinding", "-n", "backend", "robert-rolebinding")
	if err != nil {
		nt.T.Fatal("robert-rolebinding not found")
	}
}

func manageNamespace(nt *nomostest.NT, namespace string) {
	nt.T.Log("Add an unmanaged resource into the namespace as a control")
	nt.T.Log("We should never modify this resource")
	_, err := nt.Kubectl("apply", "-f", fmt.Sprintf("%s/reserved_namespaces/unmanaged-service.%s.yaml", yamlDir, namespace))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add resource to manage")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/reserved_namespaces/service.yaml", yamlDir), fmt.Sprintf("acme/namespaces/%s/service.yaml", namespace))
	nt.T.Log("Start managing the namespace")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/reserved_namespaces/namespace.%s.yaml", yamlDir, namespace), fmt.Sprintf("acme/namespaces/%s/namespace.yaml", namespace))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Start managing the namespace")
	nt.WaitForRepoSyncs()

	nt.T.Log("Wait until managed service appears on the cluster")
	_, err = nt.Kubectl("get", "services", "some-service", fmt.Sprintf("--namespace=%s", namespace))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Remove the namespace directory from the repo")
	nt.RootRepos[configsync.RootSyncName].Remove(fmt.Sprintf("acme/namespaces/%s", namespace))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove the namespace from the managed set of namespaces")
	nt.WaitForRepoSyncs()

	nt.T.Log("Wait until the managed resource disappears from the cluster")
	_, err = nt.Kubectl("get", "services", "some-service", fmt.Sprintf("--namespace=%s", namespace))
	if err == nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Ensure that the unmanaged service remained")
	_, err = nt.Kubectl("get", "services", "some-other-service", fmt.Sprintf("--namespace=%s", namespace))
	if err != nil {
		nt.T.Fatal(err)
	}
	_, err = nt.Kubectl("delete", "service", "some-other-service", "--ignore-not-found", fmt.Sprintf("--namespace=%s", namespace))
	if err != nil {
		nt.T.Fatal(err)
	}

}

func unmanageNamespace(nt *nomostest.NT, namespace string) {
	nt.T.Log("stop managing the system namespace")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/reserved_namespaces/unmanaged-namespace.%s.yaml", yamlDir, namespace), fmt.Sprintf("acme/namespaces/%s/namespace.yaml", namespace))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Stop managing the namespace")
	nt.WaitForRepoSyncs()
}

func TestNamespaceDefaultCanBeManaged(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)
	manageNamespace(nt, "default")
	unmanageNamespace(nt, "default")
}

func TestNamespaceKubeSystemCanBeManaged(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)
	manageNamespace(nt, "kube-system")
	unmanageNamespace(nt, "kube-system")
}

func TestNamespaceKubePublicCanBeManaged(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.SkipMultiRepo)
	manageNamespace(nt, "default")
	unmanageNamespace(nt, "kube-public")
}
