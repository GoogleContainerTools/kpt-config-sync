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
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	yamlDir = "../testdata"
)

func TestNoDefaultFieldsInNamespaceConfig(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.SkipMultiRepo)
	expectedName := "hello-world"
	expectedTime := metav1.Time{}

	nt.T.Log("Add a deployment")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding deployment")
	nt.WaitForRepoSyncs()

	nt.T.Log("Check that the deployment was created")
	if err := nt.Validate("hello-world", "dir", &appsv1.Deployment{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Check that specified field name is present and unspecified field creationTimestamp is not present")
	if err := nt.Validate("dir", "", &v1.NamespaceConfig{},
		func(o client.Object) error {
			raw := o.(*v1.NamespaceConfig).Spec.Resources[0].Versions[0].Objects[0].Raw
			dep := &appsv1.Deployment{}
			if err := json.Unmarshal(raw, &dep); err != nil {
				return errors.Errorf("Failed to get raw JSON, %s", err)
			}
			actualName := dep.GetName()
			if actualName != expectedName {
				return errors.Errorf("Expected name: %s, got: %s", expectedName, actualName)
			}
			return nil
		}, func(o client.Object) error {
			raw := o.(*v1.NamespaceConfig).Spec.Resources[0].Versions[0].Objects[0].Raw
			dep := &appsv1.Deployment{}
			if err := json.Unmarshal(raw, &dep); err != nil {
				return errors.Errorf("Failed to get raw JSON, %s", err)
			}
			actualTime := dep.GetCreationTimestamp()
			if actualTime != expectedTime {
				return errors.Errorf("Expected time: %s, got: %s", expectedTime, actualTime)
			}
			return nil
		}); err != nil {
		nt.T.Fatal(err)
	}
}

func TestRecreateSyncDeletedByUser(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.SkipMultiRepo)

	nt.T.Log("Add a deployment to a directory")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a deployment to a directory")
	nt.WaitForRepoSyncs()

	nt.T.Log("Ensure that the system created a sync for the deployment")
	if err := nt.Validate("deployment.apps", "default", &v1.Sync{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Force-delete the sync from the cluster")
	if err := nt.Delete(fake.SyncObject(kinds.Deployment().GroupKind(), core.Namespace("default"))); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Ensure that the system re-created the force-deleted sync")
	_, err := nomostest.Retry(60*time.Second, func() error {
		err := nt.Validate("deployment.apps", "default", &v1.Sync{})
		return err
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestDeletingClusterConfigRecoverable(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.SkipMultiRepo)

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add dir namespace")
	nt.WaitForRepoSyncs()

	if err := nt.Validate("dir", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Forcefully delete clusterconfigs and verify recovery")
	if err := nt.DeleteAllOf(&v1.ClusterConfig{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/cluster/admin-clusterrole.yaml", "acme/cluster/admin-clusterrole.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add admin clusterrole")
	nt.WaitForRepoSyncs()

	if err := nt.Validate("config-management-cluster-config", "", &v1.ClusterConfig{}); err != nil {
		nt.T.Fatal(err)
	}
}

func TestNamespaceGarbageCollection(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/accounting-namespace.yaml", yamlDir), "acme/namespaces/accounting/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add accounting namespace")
	nt.WaitForRepoSyncs()

	if err := nt.Validate("accounting", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/accounting/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove accounting namespace")
	nt.WaitForRepoSyncs()

	if err := nt.ValidateNotFound("accounting", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal("Namespace still exist when it should have been garbage collected")
	}
}

func TestNamespacePolicyspaceConversion(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/dir-namespace.yaml", yamlDir), "acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add dir namespace")
	nt.WaitForRepoSyncs()

	if err := nt.Validate("dir", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/dir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/subdir-namespace.yaml", yamlDir), "acme/namespaces/dir/subdir/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove dir namespace, add subdir namespace")
	nt.WaitForRepoSyncs()

	if err := nt.Validate("subdir", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal(err)
	}

	if err := nt.ValidateNotFound("dir", "", &corev1.Namespace{}); err != nil {
		nt.T.Fatal("Namespace still exist when it should have been converted")
	}
}

func TestSyncDeploymentAndReplicaSet(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

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
	if err := nt.Validate("hello-world", "dir", &appsv1.ReplicaSet{}, nomostest.HasLabel("app", "hello-world")); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Add a corresponding deployment")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add corresponding deployment")
	nt.WaitForRepoSyncs()

	nt.T.Log("check that the deployment was created")
	if err := nt.Validate("hello-world", "dir", &appsv1.Deployment{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Remove the deployment")
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/dir/deployment.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove deployment")
	// This sync may block until reconcile timeout is reached,
	// because the ReplicaSet is re-applied before deleting the Deployment.
	// So this wait timeout must be longer than the reconcile timeout (5m).
	nt.WaitForRepoSyncs()

	nt.T.Log("check that the deployment was removed and replicaset remains")
	if err := nt.ValidateNotFound("hello-world", "dir", &appsv1.Deployment{}); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate("hello-world", "dir", &appsv1.ReplicaSet{}, nomostest.HasLabel("app", "hello-world")); err != nil {
		nt.T.Fatal(err)
	}
}

func TestRolebindingsUpdated(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/namespaces/eng/backend/namespace.yaml", "acme/namespaces/eng/backend/namespace.yaml")
	nt.RootRepos[configsync.RootSyncName].Copy("../../examples/acme/namespaces/eng/backend/bob-rolebinding.yaml", "acme/namespaces/eng/backend/br.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add bob rolebinding")
	nt.WaitForRepoSyncs()
	if err := nt.Validate("bob-rolebinding", "backend", &rbacv1.RoleBinding{}, nomostest.RoleBindingHasName("acme-admin")); err != nil {
		nt.T.Fatal("bob-rolebinding not found")
	}

	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/robert-rolebinding.yaml", yamlDir), "acme/namespaces/eng/backend/br.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Replace bob with robert rolebinding")
	nt.WaitForRepoSyncs()

	if err := nt.ValidateNotFound("bob-rolebinding", "backend", &rbacv1.RoleBinding{}); err != nil {
		nt.T.Fatal("bob-rolebinding is not deleted")
	}

	if err := nt.Validate("robert-rolebinding", "backend", &rbacv1.RoleBinding{}); err != nil {
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

	nt.T.Cleanup(func() {
		if err := nt.Delete(fake.ServiceObject(core.Name("some-other-service"), core.Namespace(namespace))); err != nil {
			nt.T.Fatal(err)
		}
	})

	nt.T.Log("Add resource to manage")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/reserved_namespaces/service.yaml", yamlDir), fmt.Sprintf("acme/namespaces/%s/service.yaml", namespace))
	nt.T.Log("Start managing the namespace")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/reserved_namespaces/namespace.%s.yaml", yamlDir, namespace), fmt.Sprintf("acme/namespaces/%s/namespace.yaml", namespace))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Start managing the namespace")
	nt.WaitForRepoSyncs()

	nt.T.Log("Validate managed service appears on the cluster")
	if err := nt.Validate("some-service", namespace, &corev1.Service{}); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Remove the namespace directory from the repo")
	nt.RootRepos[configsync.RootSyncName].Remove(fmt.Sprintf("acme/namespaces/%s", namespace))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove the namespace from the managed set of namespaces")
	nt.WaitForRepoSyncs()

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
	nt.T.Log("stop managing the system namespace")
	nt.RootRepos[configsync.RootSyncName].Copy(fmt.Sprintf("%s/reserved_namespaces/unmanaged-namespace.%s.yaml", yamlDir, namespace), fmt.Sprintf("acme/namespaces/%s/namespace.yaml", namespace))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Stop managing the namespace")
	nt.WaitForRepoSyncs()
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
	manageNamespace(nt, "kube-system")
	unmanageNamespace(nt, "kube-public")
}
