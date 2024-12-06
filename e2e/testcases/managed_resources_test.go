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
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/applyset"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// This file includes tests for drift correction and drift prevention.
//
// Drift Correction uses the following metadata:
//  * The "configmanagement.gke.io/managed" annotation must be set to "enabled".
//  * The "configsync.gke.io/resource-id" annotation must match the object ID.
//  * The "configsync.gke.io/manager" annotation must exists and match the RSync's ResourceManager name.
//  * The "applyset.kubernetes.io/part-of" label must match the RSync's ApplySet ID.
//
// Drift Prevention uses the following metadata:
//  * The "configmanagement.gke.io/managed" annotation must be set to "enabled".
//  * The "configsync.gke.io/resource-id" annotation must match the object ID.

// TestDriftKubectlApplyClusterScoped tests drift correction after
// cluster-scoped changes are made with kubectl.
func TestDriftKubectlApplyClusterScoped(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	rootSync2Name := "abcdef"
	rootSync1ApplySetID := applyset.IDFromSync(configsync.RootSyncName, declared.RootScope)
	rootSync2ApplySetID := applyset.IDFromSync(rootSync2Name, declared.RootScope)
	rootSync1Manager := declared.ResourceManager(declared.RootScope, configsync.RootSyncName)
	rootSync2Manager := declared.ResourceManager(declared.RootScope, rootSync2Name)

	namespace := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	/* A new test */
	ns1Obj := &corev1.Namespace{}
	ns1Obj.SetName("test-ns1")
	ns1Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_namespace_test-ns1",
		metadata.ResourceManagerKey:    rootSync1Manager,
	})
	ns1Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync1ApplySetID,
	})

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-ns1.yaml"), ns1Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err := nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns1.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns1.yaml` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD delete `test-ns1`, because it says it's managed by the
	// root-reconciler, but it's not in the source declared resources.
	err = nt.Watcher.WatchForNotFound(kinds.Namespace(), "test-ns1", "")
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	ns2Obj := &corev1.Namespace{}
	ns2Obj.SetName("test-ns2")
	ns2Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_namespace_test-ns2",
		metadata.ResourceManagerKey:    rootSync2Manager,
	})
	ns2Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync2ApplySetID,
	})

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-ns2.yaml"), ns2Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns2.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns2.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the remediator can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the namespace, since its
	// `configsync.gke.io/manager` annotation indicates it is managed by a
	// RootSync that does not exist.
	err = nt.Validate("test-ns2", "", &corev1.Namespace{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			metadata.ResourceManagerKey,
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	ns3Obj := &corev1.Namespace{}
	ns3Obj.SetName("test-ns3")
	ns3Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_namespace_test-ns3",
	})
	ns2Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync2ApplySetID,
	})

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-ns3.yaml"), ns3Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns3.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns3.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the remediator can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the namespace, since it does not have a
	// `configsync.gke.io/manager` annotation.
	err = nt.Validate("test-ns3", "", &corev1.Namespace{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			// no manager
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	ns4Obj := &corev1.Namespace{}
	ns4Obj.SetName("test-ns4")
	ns4Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_namespace_wrong-ns4",
		metadata.ResourceManagerKey:    rootSync1Manager,
	})
	ns2Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync2ApplySetID,
	})

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-ns4.yaml"), ns4Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns4.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns4.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the remediator can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the namespace, since its
	// `configsync.gke.io/resource-id` annotation is incorrect.
	err = nt.Validate("test-ns4", "", &corev1.Namespace{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			metadata.ResourceManagerKey,
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	ns5Obj := &corev1.Namespace{}
	ns5Obj.SetName("test-ns5")
	ns5Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_namespace_test-ns5",
		metadata.ResourceManagerKey:    rootSync1Manager,
	})
	ns2Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: "wrong-applyset-id",
	})

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-ns5.yaml"), ns5Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-ns5.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-ns4.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the remediator can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the namespace, since its
	// `applyset.kubernetes.io/part-of` label is incorrect.
	err = nt.Validate("test-ns5", "", &corev1.Namespace{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			metadata.ResourceManagerKey,
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDriftKubectlApplyNamespaceScoped tests drift correction after
// namespace-scoped changes are made with kubectl.
func TestDriftKubectlApplyNamespaceScoped(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	rootSync2Name := "abcdef"
	rootSync1ApplySetID := applyset.IDFromSync(configsync.RootSyncName, declared.RootScope)
	rootSync2ApplySetID := applyset.IDFromSync(rootSync2Name, declared.RootScope)
	rootSync1Manager := declared.ResourceManager(declared.RootScope, configsync.RootSyncName)
	rootSync2Manager := declared.ResourceManager(declared.RootScope, rootSync2Name)

	namespace := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(rootSyncGitRepo.Add("acme/cm.yaml", k8sobjects.ConfigMapObject(core.Name("cm-1"), core.Namespace("bookstore"))))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a configmap"))
	nt.Must(nt.WatchForAllSyncs())

	/* A new test */
	cm1Obj := &corev1.ConfigMap{}
	cm1Obj.SetName("test-cm1")
	cm1Obj.SetNamespace("bookstore")
	cm1Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_configmap_bookstore_test-cm1",
		metadata.ResourceManagerKey:    rootSync1Manager,
	})
	cm1Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync1ApplySetID,
	})
	cm1Obj.Data = map[string]string{
		"weekday": "monday",
	}

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-cm1.yaml"), cm1Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err := nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm1.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm1.yaml` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD delete the configmap
	err = nt.Watcher.WatchForNotFound(kinds.ConfigMap(), "test-cm1", "bookstore")
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	cm2Obj := &corev1.ConfigMap{}
	cm2Obj.SetName("test-cm2")
	cm2Obj.SetNamespace("bookstore")
	cm2Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_configmap_bookstore_wrong-cm2",
		metadata.ResourceManagerKey:    rootSync1Manager,
	})
	cm2Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync1ApplySetID,
	})
	cm2Obj.Data = map[string]string{
		"weekday": "monday",
	}

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-cm2.yaml"), cm2Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm2.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm2.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the configmap, since its
	// `configsync.gke.io/resource-id` annotation is incorrect.
	err = nt.Validate("test-cm2", "bookstore", &corev1.ConfigMap{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			metadata.ResourceManagerKey,
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	cm3Obj := &corev1.ConfigMap{}
	cm3Obj.SetName("test-cm3")
	cm3Obj.SetNamespace("bookstore")
	cm3Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_configmap_bookstore_test-cm3",
		metadata.ResourceManagerKey:    rootSync2Manager,
	})
	cm3Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync2ApplySetID,
	})
	cm3Obj.Data = map[string]string{
		"weekday": "monday",
	}

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-cm3.yaml"), cm3Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm3.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm3.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the configmap, since its
	// `configsync.gke.io/manager` annotation indicates it is managed by a
	// RootSync that does not exist.
	err = nt.Validate("test-cm3", "bookstore", &corev1.ConfigMap{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			metadata.ResourceManagerKey,
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	cm4Obj := &corev1.ConfigMap{}
	cm4Obj.SetName("test-cm4")
	cm4Obj.SetNamespace("bookstore")
	cm4Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_configmap_bookstore_test-cm4",
	})
	cm4Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync1ApplySetID,
	})
	cm4Obj.Data = map[string]string{
		"weekday": "monday",
	}

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-cm4.yaml"), cm4Obj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm4.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm4.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the configmap, since it does not have a
	// `configsync.gke.io/manager` annotation.
	err = nt.Validate("test-cm4", "bookstore", &corev1.ConfigMap{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			// no manager
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}

	/* A new test */
	cm5Obj := &corev1.ConfigMap{}
	cm5Obj.SetName("test-cm5")
	cm5Obj.SetNamespace("bookstore")
	cm5Obj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_configmap_bookstore_test-cm5",
		metadata.ResourceManagerKey:    rootSync1Manager,
	})
	cm5Obj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: "wrong-applyset-id",
	})
	cm5Obj.Data = map[string]string{
		"weekday": "monday",
	}

	nt.Must(writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-cm5.yaml"), cm5Obj, nt.Scheme))

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-cm5.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-cm4.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the configmap, since its
	// `applyset.kubernetes.io/part-of` label is incorrect.
	nt.Must(nt.Validate("test-cm5", "bookstore", &corev1.ConfigMap{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			metadata.ResourceManagerKey,
			corev1.LastAppliedConfigAnnotation)))

	/* A new test */
	secretObj := &corev1.Secret{}
	secretObj.SetName("test-secret")
	secretObj.SetNamespace("bookstore")
	secretObj.SetAnnotations(map[string]string{
		metadata.ResourceManagementKey: metadata.ResourceManagementEnabled,
		metadata.ResourceIDKey:         "_configmap_bookstore_test-secret",
		metadata.ResourceManagerKey:    rootSync1Manager,
	})
	secretObj.SetLabels(map[string]string{
		metadata.ApplySetPartOfLabel: rootSync1ApplySetID,
	})

	if err := writeObjectYAMLFile(filepath.Join(nt.TmpDir, "test-secret.yaml"), secretObj, nt.Scheme); err != nil {
		nt.T.Fatal(err)
	}

	out, err = nt.Shell.Kubectl("apply", "-f", filepath.Join(nt.TmpDir, "test-secret.yaml"))
	if err != nil {
		nt.T.Fatalf("got `kubectl apply -f test-secret.yaml` error %v %s, want return nil", err, out)
	}

	// Wait 5 seconds so that the reconciler can process the event.
	time.Sleep(5 * time.Second)

	// Remediator SHOULD NOT delete the secret, since the GVKs of the resources
	// declared in the git repository do not include the GVK for Secret.
	err = nt.Validate("test-secret", "bookstore", &corev1.Secret{},
		testpredicates.HasExactlyAnnotationKeys(
			metadata.ResourceManagementKey,
			metadata.ResourceIDKey,
			metadata.ResourceManagerKey,
			corev1.LastAppliedConfigAnnotation))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDriftKubectlDelete deletes an object managed by Config Sync, and verifies
// that Config Sync recreates the deleted object.
func TestDriftKubectlDelete(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(rootSyncGitRepo.Add("acme/cm.yaml", k8sobjects.ConfigMapObject(core.Name("cm-1"), core.Namespace("bookstore"))))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a configmap"))
	nt.Must(nt.WatchForAllSyncs())

	nomostest.WaitForWebhookReadiness(nt)

	// Webhook SHOULD prevent kubectl from deleting a resource managed by Config Sync.
	_, err := nt.Shell.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
	if err == nil {
		nt.T.Fatalf("got `kubectl delete configmap cm-1` success, want err")
	}

	_, err = nt.Shell.Kubectl("delete", "ns", "bookstore")
	if err == nil {
		nt.T.Fatalf("got `kubectl delete ns bookstore` success, want err")
	}

	// Stop the Config Sync webhook to test the drift correction functionality
	nomostest.StopWebhook(nt)

	// Delete the configmap
	out, err := nt.Shell.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete configmap cm-1` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD recreate the configmap
	err = nt.Watcher.WatchForCurrentStatus(kinds.ConfigMap(), "cm-1", "bookstore")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the namespace
	out, err = nt.Shell.Kubectl("delete", "ns", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete ns bookstore` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD recreate the namespace
	err = nt.Watcher.WatchForCurrentStatus(kinds.Namespace(), "bookstore", "")
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDriftKubectlDeleteWithIgnoreMutationAnnotation deletes an object managed
// by Config Sync that has the `client.lifecycle.config.k8s.io/mutation`
// annotation, and verifies that Config Sync recreates the deleted object.
func TestDriftKubectlDeleteWithIgnoreMutationAnnotation(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore", core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(rootSyncGitRepo.Add("acme/cm.yaml", k8sobjects.ConfigMapObject(core.Name("cm-1"), core.Namespace("bookstore"))))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a configmap"))
	nt.Must(nt.WatchForAllSyncs())

	nomostest.WaitForWebhookReadiness(nt)

	// Webhook SHOULD prevent kubectl from deleting a resource managed by Config Sync.
	_, err := nt.Shell.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
	if err == nil {
		nt.T.Fatalf("got `kubectl delete configmap cm-1` success, want err")
	}

	_, err = nt.Shell.Kubectl("delete", "ns", "bookstore")
	if err == nil {
		nt.T.Fatalf("got `kubectl delete ns bookstore` success, want err")
	}

	// Stop the Config Sync webhook to test the drift correction functionality
	nomostest.StopWebhook(nt)

	// Delete the configmap
	out, err := nt.Shell.Kubectl("delete", "configmap", "cm-1", "-n", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete configmap cm-1` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD recreate the configmap
	err = nt.Watcher.WatchForCurrentStatus(kinds.ConfigMap(), "cm-1", "bookstore")
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the namespace
	out, err = nt.Shell.Kubectl("delete", "ns", "bookstore")
	if err != nil {
		nt.T.Fatalf("got `kubectl delete ns bookstore` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD recreate the namespace
	err = nt.Watcher.WatchForCurrentStatus(kinds.Namespace(), "bookstore", "")
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestDriftKubectlAnnotateUnmanagedField adds a new field with kubectl into a
// resource managed by Config Sync, and verifies that Config Sync
// does not remove this field.
func TestDriftKubectlAnnotateUnmanagedField(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore")
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// Add a new annotation into the namespace object
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "season=summer")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season=summer` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD NOT remove this field
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(testpredicates.HasAnnotation("season", "summer")),
		testwatcher.WatchTimeout(30*time.Second)))
	nomostest.WaitForWebhookReadiness(nt)

	// Add the `client.lifecycle.config.k8s.io/mutation` annotation into the namespace object
	// Webhook SHOULD deny the requests since this annotation is a part of the Config Sync metadata.
	ignoreMutation := fmt.Sprintf("%s=%s", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation)
	_, err = nt.Shell.Kubectl("annotate", "namespace", "bookstore", ignoreMutation)
	if err == nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s` success, want err", ignoreMutation)
	}

	// Stop the Config Sync webhook to test the drift correction functionality
	nomostest.StopWebhook(nt)

	// Add the `client.lifecycle.config.k8s.io/mutation` annotation into the namespace object
	ignoreMutation = fmt.Sprintf("%s=%s", metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation)
	out, err = nt.Shell.Kubectl("annotate", "namespace", "bookstore", ignoreMutation)
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s` error %v %s, want return nil", ignoreMutation, err, out)
	}

	//TODO: Validate if still true
	// Remediator SHOULD NOT remove this field
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation),
		),
		testwatcher.WatchTimeout(30*time.Second)))
}

// TestDriftKubectlAnnotateUnmanagedFieldWithIgnoreMutationAnnotation adds a new
// field with kubectl into a resource managed by Config Sync that has the
// `client.lifecycle.config.k8s.io/mutation` annotation, and verifies that
// Config Sync does not remove this field.
func TestDriftKubectlAnnotateUnmanagedFieldWithIgnoreMutationAnnotation(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore", core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	// Add a new annotation into the namespace object
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "season=summer")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season=summer` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD NOT remove this field
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(testpredicates.HasAnnotation("season", "summer")),
		testwatcher.WatchTimeout(30*time.Second)))
}

// TestDriftKubectlAnnotateManagedField modifies a managed field, and verifies
// that Config Sync corrects it.
func TestDriftKubectlAnnotateManagedField(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore", core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nomostest.WaitForWebhookReadiness(nt)

	// Webhook SHOULD prevent kubectl from modifying a managed field.
	_, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "--overwrite", "season=winter")
	if err == nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overrite season=winter` success, want err")
	}

	// Webhook SHOULD prevent kubectl from modifying Config Sync metadata.
	_, err = nt.Shell.Kubectl("annotate", "namespace", "bookstore", "--overwrite", fmt.Sprintf("%s=winter", metadata.ResourceManagementKey))
	if err == nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite %s=winter` success, want err", metadata.ResourceManagementKey)
	}

	// Stop the Config Sync webhook to test the drift correction functionality
	nomostest.StopWebhook(nt)

	// Modify a managed field
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "--overwrite", "season=winter")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overrite season=winter` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD correct the annotation
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(testpredicates.HasAnnotation("season", "summer"))))

	// Modify a Config Sync annotation
	out, err = nt.Shell.Kubectl("annotate", "namespace", "bookstore", "--overwrite", fmt.Sprintf("%s=winter", metadata.ResourceManagementKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore --overwrite %s=winter` error %v %s, want return nil", metadata.ResourceManagementKey, err, out)
	}

	// Remediator SHOULD correct the annotation
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
		)))
}

// TestDriftKubectlAnnotateDeleteManagedFields deletes a managed field, and
// verifies that Config Sync corrects it.
func TestDriftKubectlAnnotateDeleteManagedFields(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	namespace := k8sobjects.NamespaceObject("bookstore", core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", namespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nomostest.WaitForWebhookReadiness(nt)

	// Webhook SHOULD prevent kubectl from deleting a managed field.
	_, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "season-")
	if err == nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season-` success, want err")
	}

	// Webhook SHOULD prevent kubectl from deleting Config Sync metadata.
	_, err = nt.Shell.Kubectl("annotate", "namespace", "bookstore", fmt.Sprintf("%s-", metadata.ResourceManagementKey))
	if err == nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s-` success, want err", metadata.ResourceManagementKey)
	}

	// Stop the Config Sync webhook to test the drift correction functionality
	nomostest.StopWebhook(nt)

	// Delete a managed field
	out, err := nt.Shell.Kubectl("annotate", "namespace", "bookstore", "season-")
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore season-` error %v %s, want return nil", err, out)
	}

	// Remediator SHOULD correct it
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(testpredicates.HasAnnotation("season", "summer"))))

	// Delete a Config Sync annotation
	out, err = nt.Shell.Kubectl("annotate", "namespace", "bookstore", fmt.Sprintf("%s-", metadata.ResourceManagementKey))
	if err != nil {
		nt.T.Fatalf("got `kubectl annotate namespace bookstore %s-` error %v %s, want return nil", metadata.ResourceManagementKey, err, out)
	}

	// Remediator SHOULD correct it
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), "bookstore", "",
		testwatcher.WatchPredicates(
			testpredicates.HasAnnotation(metadata.ResourceManagementKey, metadata.ResourceManagementEnabled),
		)))
}

// TestDriftRemoveApplySetPartOfLabel deletes the
// `applyset.kubernetes.io/part-of` label, and verifies that
// Config Sync re-adds it.
func TestDriftRemoveApplySetPartOfLabel(t *testing.T) {
	nt := nomostest.New(t, nomostesting.DriftControl,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	rootSync1ApplySetID := applyset.IDFromSync(configsync.RootSyncName, declared.RootScope)

	// Stop webhook to allow mutation of managed fields on a managed object.
	// Stop webhook before new commit, to ensure subsequent apply completes.
	// TODO: fix StopWebhook to wait for deployment update and subsequent apply.
	nomostest.StopWebhook(nt)

	namespace := "bookstore-2"

	nsObj := k8sobjects.NamespaceObject(namespace,
		core.Annotation("season", "summer"))
	nt.Must(rootSyncGitRepo.Add("acme/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("add a namespace"))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Log("Changing the ApplySet ID label value")
	nsObj = k8sobjects.NamespaceObject(namespace)
	nt.Must(nt.KubeClient.MergePatch(nsObj,
		fmt.Sprintf(`{"metadata": {"labels": {%q: %q}, "annotations": {"season": "winter"}}}`,
			metadata.ApplySetPartOfLabel, "wrong-applyset-id")))

	// Remediator SHOULD correct it
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace, "", testwatcher.WatchPredicates(
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, rootSync1ApplySetID),
		testpredicates.HasAnnotation("season", "summer"),
	), testwatcher.WatchTimeout(10*time.Second)))

	nt.T.Log("Removing the ApplySet ID label")
	nsObj = k8sobjects.NamespaceObject(namespace)
	nt.Must(nt.KubeClient.MergePatch(nsObj,
		fmt.Sprintf(`{"metadata": {"labels": {%q: null}, "annotations": {"season": "winter"}}}`,
			metadata.ApplySetPartOfLabel)))

	// Remediator SHOULD correct it
	nt.Must(nt.Watcher.WatchObject(kinds.Namespace(), namespace, "", testwatcher.WatchPredicates(
		testpredicates.HasLabel(metadata.ApplySetPartOfLabel, rootSync1ApplySetID),
		testpredicates.HasAnnotation("season", "summer"),
	), testwatcher.WatchTimeout(10*time.Second)))
}

func writeObjectYAMLFile(path string, obj client.Object, scheme *runtime.Scheme) error {
	nsBytes, err := testkubeclient.SerializeObject(obj, ".yaml", scheme)
	if err != nil {
		return fmt.Errorf("failed to format yaml: %w", err)
	}
	if err := os.WriteFile(path, nsBytes, 0644); err != nil {
		return fmt.Errorf("failed to create a tmp file: %w", err)
	}
	return nil
}
