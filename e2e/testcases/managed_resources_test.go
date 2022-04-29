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
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync"

	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
)

// This file includes tests for drift correction and drift prevention.
//
// The drift correction in the mono-repo mode utilizes the following two annotations:
//  * configmanagement.gke.io/managed
//  * configsync.gke.io/resource-id
//
// The drift correction in the multi-repo mode utilizes the follwoing three annotations:
//  * configmanagement.gke.io/managed
//  * configsync.gke.io/resource-id
//  * configsync.gke.io/manager
//
// The drift prevention is only supported in the multi-repo mode, and utilizes the following Config Sync metadata:
//  * the configmanagement.gke.io/managed annotation
//  * the configsync.gke.io/resource-id annotation
//  * the configsync.gke.io/delcared-version label

// The reason we have both TestKubectlCreatesManagedNamespaceResourceMonoRepo and
// TestKubectlCreatesManagedNamespaceResourceMultiRepo is that the mono-repo mode and
// CSMR handles managed namespaces which are created by other parties differently:
//   * the mono-repo mode does not remove these namespaces;
//   * CSMR does remove these namespaces.

// TestKubectlCreatesManagedNamespaceResourceMonoRepo tests the drift correction regarding kubectl
// tries to create a managed namespace in the the mono-repo mode.
func TestKubectlCreatesManagedNamespaceResourceMonoRepo(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMultiRepo, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore")
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	ns := []byte(`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _namespace_test-ns
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-ns.yaml"), ns, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err := nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Config Sync should not modify the namespace.
	err = nt.Validate("test-ns", "", &corev1.Namespace{}, nomostest.HasExactlyAnnotationKeys(
		metadata.ResourceManagementKey, metadata.ResourceIDKey, "kubectl.kubernetes.io/last-applied-configuration"))
	if err != nil {
		nt.T.Fatal(err)
	}

	ns = []byte(`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _namespace_wrong-ns
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-ns.yaml"), ns, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Config Sync should not modify the namespace, since its `configsync.gke.io/resource-id`
	// annotation is incorrect.
	err = nt.Validate("test-ns", "", &corev1.Namespace{}, nomostest.HasExactlyAnnotationKeys(
		metadata.ResourceManagementKey, metadata.ResourceIDKey, "kubectl.kubernetes.io/last-applied-configuration"))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestKubectlCreatesManagedNamespaceResourceMultiRepo tests the drift correction regarding kubectl
// tries to create a managed namespace in the the multi-repo mode.
func TestKubectlCreatesManagedNamespaceResourceMultiRepo(t *testing.T) {
	nt := nomostest.New(t, ntopts.SkipMonoRepo, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore")
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	/* A new test */
	ns := []byte(`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns1
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _namespace_test-ns1
    configsync.gke.io/manager: :root
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-ns1.yaml"), ns, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err := nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns1.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns1.yaml` error %v %s, want return nil", err, out)
	}

	// Config Sync should remove `test-ns1`.
	nomostest.WaitToTerminate(nt, kinds.Namespace(), "test-ns1", "")

	/* A new test */
	ns = []byte(`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns2
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _namespace_test-ns2
    configsync.gke.io/manager: :abcdef
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-ns2.yaml"), ns, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns2.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns2.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the remediator can process the event.
	time.Sleep(5 * time.Second)

	// The `configsync.gke.io/manager` annotation of `test-ns2` suggests that its manager is ':abcdef'.
	// The root reconciler does not manage `test-ns2`, therefore should not remove `test-ns2`.
	err = nt.Validate("test-ns2", "", &corev1.Namespace{}, nomostest.HasExactlyAnnotationKeys(
		metadata.ResourceManagementKey, metadata.ResourceIDKey, metadata.ResourceManagerKey, "kubectl.kubernetes.io/last-applied-configuration"))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	ns = []byte(`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns3
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _namespace_test-ns3
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-ns3.yaml"), ns, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns3.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns3.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the remediator can process the event.
	time.Sleep(5 * time.Second)

	// Config Sync should not modify the namespace, since it does not have a `configsync.gke.io/manager` annotation.
	err = nt.Validate("test-ns3", "", &corev1.Namespace{}, nomostest.HasExactlyAnnotationKeys(
		metadata.ResourceManagementKey, metadata.ResourceIDKey, "kubectl.kubernetes.io/last-applied-configuration"))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	ns = []byte(`
apiVersion: v1
kind: Namespace
metadata:
  name: test-ns4
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _namespace_wrong-ns4
    configsync.gke.io/manager: :root
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-ns4.yaml"), ns, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns4.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns4.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the remediator can process the event.
	time.Sleep(5 * time.Second)

	// Config Sync should not modify the namespace, since its `configsync.gke.io/resource-id`
	// annotation is incorrect.
	err = nt.Validate("test-ns4", "", &corev1.Namespace{}, nomostest.HasExactlyAnnotationKeys(
		metadata.ResourceManagementKey, metadata.ResourceIDKey, metadata.ResourceManagerKey, "kubectl.kubernetes.io/last-applied-configuration"))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestKubectlCreatesManagedConfigMapResource tests the drift correction regarding kubectl
// tries to create a managed non-namespace resource in both the mono-repo mode and the multi-repo mode.
func TestKubectlCreatesManagedConfigMapResource(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore")
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	nt.RootRepos[configsync.RootSyncName].Add("acme/cm.yaml", fake.ConfigMapObject(core.Name("cm-1"), core.Namespace("bookstore")))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a configmap")
	nt.WaitForRepoSyncs()

	/* A new test */
	cm := []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm1
  namespace: bookstore
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _configmap_bookstore_test-cm1
    configsync.gke.io/manager: :root
data:
  weekday: "monday"
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-cm1.yaml"), cm, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err := nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm1.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm1.yaml` error %v %s, want return nil", err, out)
	}

	// Config Sync should remove `test-ns`.
	nomostest.WaitToTerminate(nt, kinds.ConfigMap(), "test-cm1", "bookstore")

	/* A new test */
	cm = []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm2
  namespace: bookstore
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _configmap_bookstore_wrong-cm2
    configsync.gke.io/manager: :root
data:
  weekday: "monday"
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-cm2.yaml"), cm, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm2.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm2.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Config Sync should not modify the configmap, since its `configsync.gke.io/resource-id`
	// annotation is incorrect.
	err = nt.Validate("test-cm2", "bookstore", &corev1.ConfigMap{}, nomostest.HasExactlyAnnotationKeys(
		metadata.ResourceManagementKey, metadata.ResourceIDKey, metadata.ResourceManagerKey, "kubectl.kubernetes.io/last-applied-configuration"))
	if err != nil {
		nt.T.Fatal(err)
	}

	if nt.MultiRepo {
		/* A new test */
		cm = []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm3
  namespace: bookstore
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _configmap_bookstore_test-cm3
    configsync.gke.io/manager: :abcdef
data:
  weekday: "monday"
`)

		if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-cm3.yaml"), cm, 0644); err != nil {
			nt.T.Fatalf("failed to create a tmp file %v", err)
		}

		out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm3.yaml"))
		if err != nil {
			nt.T.Fatalf("got `kubectl apply -f test-cm3.yaml` error %v %s, want return nil", err, out)
		}

		// Wait 5 seconds so that the reconciler can process the event.
		time.Sleep(5 * time.Second)

		// The `configsync.gke.io/manager` annotation of `test-ns3` suggests that its manager is ':abcdef'.
		// The root reconciler does not manage `test-ns3`, therefore should not remove `test-ns3`.
		err = nt.Validate("test-cm3", "bookstore", &corev1.ConfigMap{}, nomostest.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey, metadata.ResourceIDKey, metadata.ResourceManagerKey, "kubectl.kubernetes.io/last-applied-configuration"))
		if err != nil {
			nt.T.Fatal(err)
		}

		/* A new test */
		cm = []byte(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm4
  namespace: bookstore
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _configmap_bookstore_test-cm4
data:
  weekday: "monday"
`)

		if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-cm4.yaml"), cm, 0644); err != nil {
			nt.T.Fatalf("failed to create a tmp file %v", err)
		}

		out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm4.yaml"))
		if err != nil {
			nt.T.Fatalf("got `kubectl apply -f test-cm4.yaml` error %v %s, want return nil", err, out)
		}

		// Wait 5 seconds so that the reconciler can process the event.
		time.Sleep(5 * time.Second)

		// Config Sync should not modify the configmap, since it does not have a `configsync.gke.io/manager` annotation.
		err = nt.Validate("test-cm4", "bookstore", &corev1.ConfigMap{}, nomostest.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey, metadata.ResourceIDKey, "kubectl.kubernetes.io/last-applied-configuration"))
		if err != nil {
			nt.T.Fatal(err)
		}
	}

	/* A new test */
	cm = []byte(`
apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: bookstore
  annotations:
    configmanagement.gke.io/managed: enabled
    configsync.gke.io/resource-id: _configmap_bookstore_test-secret
    configsync.gke.io/manager: :root
`)

	if err := ioutil.WriteFile(filepath.Join(nt.TmpDir, "test-secret.yaml"), cm, 0644); err != nil {
		nt.T.Fatalf("failed to create a tmp file %v", err)
	}

	out, err = nt.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-secret.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-secret.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Config Sync should not modify the secret, since the GVKs of the resources declared in the git repository
	// do not include the GVK for Secret.
	err = nt.Validate("test-secret", "bookstore", &corev1.Secret{}, nomostest.HasExactlyAnnotationKeys(
		metadata.ResourceManagementKey, metadata.ResourceIDKey, metadata.ResourceManagerKey, "kubectl.kubernetes.io/last-applied-configuration"))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDeleteManagedResources deletes an object managed by Config Sync,
// and verifies that Config Sync recreates the deleted object.
func TestDeleteManagedResources(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore")
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	nt.RootRepos[configsync.RootSyncName].Add("acme/cm.yaml", fake.ConfigMapObject(core.Name("cm-1"), core.Namespace("bookstore")))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a configmap")
	nt.WaitForRepoSyncs()

	if nt.MultiRepo {
		nomostest.WaitForWebhookReadiness(nt)

		// At this point, the Config Sync webhook is on, and should prevent kubectl from deleting a resource managed by Config Sync.
		_, err := nt.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
		if err == nil {
			nt.T.Fatalf("got `kubectl delete configmap cm-1` successs, want err")
		}

		_, err = nt.Kubectl("delete", "ns", "bookstore")
		if err == nil {
			nt.T.Fatalf("got `kubectl delete ns bookstore` success, want err")
		}

		// Stop the Config Sync webhook to test the drift correction functionality
		nomostest.StopWebhook(nt)
	}

	// Delete the configmap
	out, err := nt.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete configmap cm-1` error %v %s, want return nil", err, out)
	}

	// Verify Config Sync recreates the configmap
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("cm-1", "bookstore", &corev1.ConfigMap{})
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the namespace
	out, err = nt.Kubectl("delete", "ns", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete ns bookstore` error %v %s, want return nil", err, out)
	}

	// Verify Config Sync recreates the namespace
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{})
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDeleteManagedResourcesWithIgnoreMutationAnnotation deletes an object managed by Config Sync
// and having the `client.lifecycle.config.k8s.io/mutation` annotation,
// and verifies that Config Sync recreates the deleted object.
func TestDeleteManagedResourcesWithIgnoreMutationAnnotation(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore", core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	nt.RootRepos[configsync.RootSyncName].Add("acme/cm.yaml", fake.ConfigMapObject(core.Name("cm-1"), core.Namespace("bookstore")))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a configmap")
	nt.WaitForRepoSyncs()

	if nt.MultiRepo {
		nomostest.WaitForWebhookReadiness(nt)

		// At this point, the Config Sync webhook is on, and should prevent kubectl from deleting a resource managed by Config Sync.
		_, err := nt.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
		if err == nil {
			nt.T.Fatalf("got `kubectl delete configmap cm-1` successs, want err")
		}

		_, err = nt.Kubectl("delete", "ns", "bookstore")
		if err == nil {
			nt.T.Fatalf("got `kubectl delete ns bookstore` success, want err")
		}

		// Stop the Config Sync webhook to test the drift correction functionality
		nomostest.StopWebhook(nt)
	}

	// Delete the configmap
	out, err := nt.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete configmap cm-1` error %v %s, want return nil", err, out)
	}

	// Verify Config Sync recreates the configmap
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("cm-1", "bookstore", &corev1.ConfigMap{})
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the namespace
	out, err = nt.Kubectl("delete", "ns", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete ns bookstore` error %v %s, want return nil", err, out)
	}

	// Verify Config Sync recreates the namespace
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{})
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestAddFieldsIntoManagedResources adds a new field with kubectl into a resource
// managed by Config Sync, and verifies that Config Sync does not remove this field.
func TestAddFieldsIntoManagedResources(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore")
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	// Add a new annotation into the namespace object
	out, err := nt.Kubectl("annotate", "namespace", "bookstore", "season=summer")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season=summer` error %v %s, want return nil", err, out)
	}

	// Verify Config Sync does not remove this field
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation("season", "summer"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
	if nt.MultiRepo {
		nomostest.WaitForWebhookReadiness(nt)

		// Add the `client.lifecycle.config.k8s.io/mutation` annotation into the namespace object
		// The webhook should deny the requests since this annotation is a part of the Config Sync metadata.
		ignoreMutation := fmt.Sprintf("%s=%s", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation)
		_, err = nt.Kubectl("annotate", "namespace", "bookstore", ignoreMutation)
		if err == nil {
			nt.T.Fatalf("got `kubectl annotate namespace bookstore %s` success, want err", ignoreMutation)
		}

		// Stop the Config Sync webhook to test the drift correction functionality
		nomostest.StopWebhook(nt)
	}

	// Add the `client.lifecycle.config.k8s.io/mutation` annotation into the namespace object
	ignoreMutation := fmt.Sprintf("%s=%s", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation)
	out, err = nt.Kubectl("annotate", "namespace", "bookstore", ignoreMutation)
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s` error %v %s, want return nil", ignoreMutation, err, out)
	}

	// Verify Config Sync does not remove this field
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestAddFieldsIntoManagedResourcesWithIgnoreMutationAnnotation adds a new field with kubectl into a resource
// managed by Config Sync and having the `client.lifecycle.config.k8s.io/mutation` annotation,
// and verifies that Config Sync does not remove this field.
func TestAddFieldsIntoManagedResourcesWithIgnoreMutationAnnotation(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore", core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	// Add a new annotation into the namespace object
	out, err := nt.Kubectl("annotate", "namespace", "bookstore", "season=summer")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season=summer` error %v %s, want return nil", err, out)
	}

	// Verify Config Sync does not remove this field
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation("season", "summer"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestModifyManagedFields modifies a managed field, and verifies that Config Sync corrects it.
func TestModifyManagedFields(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore", core.Annotation("season", "summer"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	if nt.MultiRepo {
		nomostest.WaitForWebhookReadiness(nt)

		// At this point, the Config Sync webhook is on, and should prevent kubectl from modifying a managed field.
		_, err := nt.Kubectl("annotate", "namespace", "bookstore", "--overwrite", "season=winter")
		if err == nil {
			nt.T.Fatalf("got `kubectl annotate namespace bookstore --overrite season=winter` success, want err")
		}

		// At this point, the Config Sync webhook is on, and should prevent kubectl from modifying Config Sync metadata.
		_, err = nt.Kubectl("annotate", "namespace", "bookstore", "--overwrite", fmt.Sprintf("%s=winter", metadata.ResourceManagementKey))
		if err == nil {
			nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite %s=winter` success, want err", metadata.ResourceManagementKey)
		}

		// Stop the Config Sync webhook to test the drift correction functionality
		nomostest.StopWebhook(nt)
	}

	// Modify a managed field
	out, err := nt.Kubectl("annotate", "namespace", "bookstore", "--overwrite", "season=winter")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overrite season=winter` error %v %s, want return nil", err, out)
	}

	// Verify Config Sync corrects it
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation("season", "summer"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Modify a Config Sync annotation
	out, err = nt.Kubectl("annotate", "namespace", "bookstore", "--overwrite", fmt.Sprintf("%s=winter", metadata.ResourceManagementKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite %s=winter` error %v %s, want return nil", metadata.ResourceManagementKey, err, out)
	}

	// Verify Config Sync corrects it
	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestModifyManagedFieldsWithIgnoreMutationAnnotation modifies a managed field of a resource having
// the `client.lifecycle.config.k8s.io/mutation` annotation, and verifies that Config Sync does not correct it.
func TestModifyManagedFieldsWithIgnoreMutationAnnotation(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.SkipMonoRepo)

	namespace := fake.NamespaceObject("bookstore",
		core.Annotation("season", "summer"),
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	// Modify a managed field
	out, err := nt.Kubectl("annotate", "namespace", "bookstore", "--overwrite", "season=winter")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overrite season=winter` error %v %s, want return nil", err, out)
	}

	time.Sleep(10 * time.Second)

	// Verify Config Sync does not correct it
	err = nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation("season", "winter"))
	if err != nil {
		nt.T.Fatal(err)
	}

	if nt.MultiRepo {
		// The reason we need to stop the webhook here is that the webhook denies a request to modify Config Sync metadata
		// even if the resource has the `client.lifecycle.config.k8s.io/mutation` annotation.
		nomostest.StopWebhook(nt)
	}

	// Modify a Config Sync annotation
	out, err = nt.Kubectl("annotate", "namespace", "bookstore", "--overwrite", fmt.Sprintf("%s=winter", metadata.ResourceManagementKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite %s=winter` error %v %s, want return nil", metadata.ResourceManagementKey, err, out)
	}

	time.Sleep(10 * time.Second)

	// Verify Config Sync does not correct it
	err = nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation(metadata.ResourceManagementKey, "winter"))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDeleteManagedFields deletes a managed field, and verifies that Config Sync corrects it.
func TestDeleteManagedFields(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured)

	namespace := fake.NamespaceObject("bookstore", core.Annotation("season", "summer"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	if nt.MultiRepo {
		nomostest.WaitForWebhookReadiness(nt)

		// At this point, the Config Sync webhook is on, and should prevent kubectl from deleting a managed field.
		_, err := nt.Kubectl("annotate", "namespace", "bookstore", "season-")
		if err == nil {
			nt.T.Fatalf("got `kubectl annotate namespace bookstore season-` success, want err")
		}

		// At this point, the Config Sync webhook is on, and should prevent kubectl from deleting Config Sync metadata.
		_, err = nt.Kubectl("annotate", "namespace", "bookstore", fmt.Sprintf("%s-", metadata.ResourceManagementKey))
		if err == nil {
			nt.T.Fatalf("got `kubectl annotate namespace bookstore %s-` success, want err", metadata.ResourceManagementKey)
		}

		// Stop the Config Sync webhook to test the drift correction functionality
		nomostest.StopWebhook(nt)
	}

	// Delete a managed field
	out, err := nt.Kubectl("annotate", "namespace", "bookstore", "season-")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season-` error %v %s, want return nil", err, out)
	}

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation("season", "summer"))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete a Config Sync annotation
	out, err = nt.Kubectl("annotate", "namespace", "bookstore", fmt.Sprintf("%s-", metadata.ResourceManagementKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s-` error %v %s, want return nil", metadata.ResourceManagementKey, err, out)
	}

	_, err = nomostest.Retry(30*time.Second, func() error {
		return nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.HasAnnotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled))
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDeleteManagedFieldsWithIgnoreMutationAnnotation deletes a managed field of a resource having
// the `client.lifecycle.config.k8s.io/mutation` annotation, and verifies that Config Sync does not correct it.
func TestDeleteManagedFieldsWithIgnoreMutationAnnotation(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.SkipMonoRepo)

	namespace := fake.NamespaceObject("bookstore",
		core.Annotation("season", "summer"),
		core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add a namespace")
	nt.WaitForRepoSyncs()

	// Delete a managed field
	out, err := nt.Kubectl("annotate", "namespace", "bookstore", "season-")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season-` error %v %s, want return nil", err, out)
	}

	time.Sleep(10 * time.Second)
	// Verify Config Sync does not correct it
	err = nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.MissingAnnotation("season"))
	if err != nil {
		nt.T.Fatal(err)
	}

	if nt.MultiRepo {
		// The reason we need to stop the webhook here is that the webhook denies a request to modify Config Sync metadata
		// even if the resource has the `client.lifecycle.config.k8s.io/mutation` annotation.
		nomostest.StopWebhook(nt)
	}

	// Delete a Config Sync annotation
	out, err = nt.Kubectl("annotate", "namespace", "bookstore", fmt.Sprintf("%s-", metadata.ResourceManagementKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s-` error %v %s, want return nil", metadata.ResourceManagementKey, err, out)
	}

	time.Sleep(10 * time.Second)
	// Verify Config Sync does not correct it
	err = nt.Validate("bookstore", "", &corev1.Namespace{}, nomostest.MissingAnnotation(metadata.ResourceManagementKey))
	if err != nil {
		nt.T.Fatal(err)
	}
}
