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
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestDeclareNamespace runs a test that ensures ACM syncs Namespaces to clusters.
func TestDeclareNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	err := nt.ValidateNotFound("foo", "", &corev1.Namespace{})
	if err != nil {
		// Failed test precondition.
		nt.T.Fatal(err)
	}

	nsObj := k8sobjects.NamespaceObject("foo")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("add Namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the Namespace "foo" exists.
	err = nt.Validate("foo", "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestNamespaceLabelAndAnnotationLifecycle(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Create foo namespace without any labels or annotations.
	nsObj := k8sobjects.NamespaceObject("foo")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Create foo namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the namespace exists.
	err := nt.Validate(nsObj.Name, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add label and annotation to namespace.
	nsObj.Labels["label"] = "test-label"
	nsObj.Annotations["annotation"] = "test-annotation"
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Updated foo namespace to include label and annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the namespace exists with label and annotation.
	err = nt.Validate(nsObj.Name, "", &corev1.Namespace{}, testpredicates.HasLabel("label", "test-label"), testpredicates.HasAnnotation("annotation", "test-annotation"))
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update label and annotation to namespace.
	nsObj.Labels["label"] = "updated-test-label"
	nsObj.Annotations["annotation"] = "updated-test-annotation"
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Updated foo namespace to include label and annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the namespace exists with the updated label and annotation.
	err = nt.Validate(nsObj.Name, "", &corev1.Namespace{}, testpredicates.HasLabel("label", "updated-test-label"), testpredicates.HasAnnotation("annotation", "updated-test-annotation"))
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove label and annotation to namespace and commit.
	delete(nsObj.Labels, "label")
	delete(nsObj.Annotations, "annotation")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", nsObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("Updated foo namespace, removing label and annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the namespace exists without the label and annotation.
	err = nt.Validate(nsObj.Name, "", &corev1.Namespace{}, testpredicates.MissingLabel("label"), testpredicates.MissingAnnotation("annotation"))
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestNamespaceExistsAndDeclared(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Create nsObj using kubectl first then commit.
	nsObj := k8sobjects.NamespaceObject("decl-namespace-annotation-none")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/decl-namespace-annotation-none/ns.yaml", nsObj))
	nt.MustKubectl("apply", "-f", filepath.Join(rootSyncGitRepo.Root, "acme/namespaces/decl-namespace-annotation-none/ns.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add namespace"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the namespace exists after sync.
	err := nt.Validate(nsObj.Name, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestNamespaceEnabledAnnotationNotDeclared(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Create nsObj with managed annotation using kubectl.
	nsObj := k8sobjects.NamespaceObject("undeclared-annotation-enabled")
	nsObj.Annotations["configmanagement.gke.io/managed"] = "enabled"
	nt.Must(rootSyncGitRepo.Add("ns.yaml", nsObj))
	nt.MustKubectl("apply", "-f", filepath.Join(rootSyncGitRepo.Root, "ns.yaml"))
	nt.Must(rootSyncGitRepo.Remove("ns.yaml"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the namespace exists after sync.
	err := nt.Validate(nsObj.Name, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync:        nomostest.RootSyncNN(configsync.RootSyncName),
		ObjectCount: 0, // test Namespace not committed
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestManagementDisabledNamespace tests https://cloud.google.com/anthos-config-management/docs/how-to/managing-objects#unmanaged-namespaces.
func TestManagementDisabledNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	checkpointProtectedNamespace(nt, metav1.NamespaceDefault)

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)

	namespacesToTest := []string{"foo", metav1.NamespaceDefault}
	for _, nsName := range namespacesToTest {
		// Create nsObj.
		nsObj := k8sobjects.NamespaceObject(nsName)
		cm1 := k8sobjects.ConfigMapObject(core.Namespace(nsName), core.Name("cm1"))
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", nsName), nsObj))
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/cm1.yaml", nsName), cm1))
		nt.Must(rootSyncGitRepo.CommitAndPush("Create a namespace and a configmap"))
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}

		// Test that the namespace exists with expected config management labels and annotations.
		err := nt.Validate(nsObj.Name, "", &corev1.Namespace{}, testpredicates.HasAllNomosMetadata())
		if err != nil {
			nt.T.Fatal(err)
		}

		// Test that the configmap exists with expected config management labels and annotations.
		err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, testpredicates.HasAllNomosMetadata())
		if err != nil {
			nt.T.Fatal(err)
		}

		nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
		nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, cm1)

		err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
			Sync: nomostest.RootSyncNN(configsync.RootSyncName),
		})
		if err != nil {
			nt.T.Fatal(err)
		}

		// Update the namespace and the configmap to be no longer be managed
		nsObj.Annotations[metadata.ResourceManagementKey] = metadata.ResourceManagementDisabled
		cm1.Annotations[metadata.ResourceManagementKey] = metadata.ResourceManagementDisabled
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", nsName), nsObj))
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("acme/namespaces/%s/cm1.yaml", nsName), cm1))
		nt.Must(rootSyncGitRepo.CommitAndPush("Unmanage the namespace and the configmap"))
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}

		// Test that the now unmanaged namespace does not contain any config management labels or annotations
		err = nt.Validate(nsObj.Name, "", &corev1.Namespace{}, testpredicates.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Fatal(err)
		}

		// Test that the now unmanaged configmap does not contain any config management labels or annotations
		err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, testpredicates.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Fatal(err)
		}

		nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
		nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, cm1)

		err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
			Sync: nomostest.RootSyncNN(configsync.RootSyncName),
		})
		if err != nil {
			nt.T.Fatal(err)
		}

		// Remove the namspace and the configmap from the repository
		nt.Must(rootSyncGitRepo.Remove(fmt.Sprintf("acme/namespaces/%s", nsName)))
		nt.Must(rootSyncGitRepo.CommitAndPush("Remove the namespace and the configmap"))
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}

		// Test that the namespace still exists on the cluster, and does not contain any config management labels or annotations
		err = nt.Validate(nsObj.Name, "", &corev1.Namespace{}, testpredicates.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Fatal(err)
		}

		// Test that the configmap still exists on the cluster, and does not contain any config management labels or annotations
		err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, testpredicates.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Fatal(err)
		}

		nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, nsObj)
		nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, cm1)

		err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
			Sync: nomostest.RootSyncNN(configsync.RootSyncName),
		})
		if err != nil {
			nt.T.Fatal(err)
		}
	}
}

// TestManagementDisabledConfigMap tests https://cloud.google.com/anthos-config-management/docs/how-to/managing-objects#stop-managing.
func TestManagementDisabledConfigMap(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	fooNamespace := k8sobjects.NamespaceObject("foo")
	cm1 := k8sobjects.ConfigMapObject(core.Namespace("foo"), core.Name("cm1"))
	// Initialize repo with disabled resource to test initial sync w/ unmanaged resources
	cm2 := k8sobjects.ConfigMapObject(core.Namespace("foo"), core.Name("cm2"), core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementDisabled))
	cm3 := k8sobjects.ConfigMapObject(core.Namespace("foo"), core.Name("cm3"))

	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.WithInitialCommit(ntopts.Commit{
		Message: "Create namespace and configmaps",
		Files: map[string]client.Object{
			"acme/namespaces/foo/ns.yaml":  fooNamespace,
			"acme/namespaces/foo/cm1.yaml": cm1,
			"acme/namespaces/foo/cm2.yaml": cm2,
			"acme/namespaces/foo/cm3.yaml": cm3,
		},
	}))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Test that the namespace exists with expected config management labels and annotations.
	err := nt.Validate(fooNamespace.Name, "", &corev1.Namespace{}, testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Test that cm1 exists with expected config management labels and annotations.
	err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Test that the unmanaged cm2 does not exist.
	err = nt.ValidateNotFound(cm2.Name, cm2.Namespace, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Error(err)
	}

	// Test that cm3 exists with expected config management labels and annotations.
	err = nt.Validate(cm3.Name, cm3.Namespace, &corev1.ConfigMap{}, testpredicates.HasAllNomosMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
		// Default object count and operations include objects added with `WithInitialCommit`
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Fail if any validations errored
	if nt.T.Failed() {
		nt.T.FailNow()
	}

	// Update the configmap to be no longer be managed
	cm1.Annotations[metadata.ResourceManagementKey] = metadata.ResourceManagementDisabled
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/cm1.yaml", cm1))
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/foo/cm3.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Unmanage cm1 and remove cm3"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the now unmanaged configmap does not contain any config management labels or annotations
	err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, testpredicates.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Test that cm3 was properly pruned.
	err = nt.ValidateNotFound(cm3.Name, cm3.Namespace, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, cm3)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Fail if any validations errored
	if nt.T.Failed() {
		nt.T.FailNow()
	}

	// Remove the configmap from the repository
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/foo/cm1.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove the configmap"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the configmap still exists on the cluster, and does not contain any config management labels or annotations
	err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, testpredicates.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Abandoned object is removed but not deleted
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, cm1)

	// Validate metrics.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
		// Adjust operations for this edge case.
		// The abandoned object is NOT in the declared objects but it does get
		// updated to remove config sync annotations and labels.
		Operations: []metrics.ObjectOperation{
			{Operation: metrics.UpdateOperation, Count: 1}, // ConfigMap
		},
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Fail if any validations errored
	if nt.T.Failed() {
		nt.T.FailNow()
	}
}

func TestSyncLabelsAndAnnotationsOnKubeSystem(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.SkipAutopilotCluster)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	checkpointProtectedNamespace(nt, metav1.NamespaceSystem)

	// Update kube-system namespace to be managed.
	kubeSystemNamespace := k8sobjects.NamespaceObject(metav1.NamespaceSystem)
	kubeSystemNamespace.Labels["test-corp.com/awesome-controller-flavour"] = "fuzzy"
	kubeSystemNamespace.Annotations["test-corp.com/awesome-controller-mixin"] = "green"
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/kube-system/ns.yaml", kubeSystemNamespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the kube-system namespace exists with label and annotation.
	err := nt.Validate(kubeSystemNamespace.Name, "", &corev1.Namespace{},
		testpredicates.HasLabel("test-corp.com/awesome-controller-flavour", "fuzzy"),
		testpredicates.HasAnnotation("test-corp.com/awesome-controller-mixin", "green"),
	)
	if err != nil {
		nt.T.Error(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, kubeSystemNamespace)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove label and annotation from the kube-system namespace.
	delete(kubeSystemNamespace.Labels, "test-corp.com/awesome-controller-flavour")
	delete(kubeSystemNamespace.Annotations, "test-corp.com/awesome-controller-mixin")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/kube-system/ns.yaml", kubeSystemNamespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("Remove label and annotation"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the kube-system namespace exists without the label and annotation.
	err = nt.Validate(kubeSystemNamespace.Name, "", &corev1.Namespace{},
		testpredicates.MissingLabel("test-corp.com/awesome-controller-flavour"), testpredicates.MissingAnnotation("test-corp.com/awesome-controller-mixin"))
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, kubeSystemNamespace)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update kube-system namespace to be no longer be managed.
	kubeSystemNamespace.Annotations["configmanagement.gke.io/managed"] = "disabled"
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/kube-system/ns.yaml", kubeSystemNamespace))
	nt.Must(rootSyncGitRepo.CommitAndPush("Update namespace to no longer be managed"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the now unmanaged kube-system namespace does not contain any config management labels or annotations.
	err = nt.Validate(kubeSystemNamespace.Name, "", &corev1.Namespace{}, testpredicates.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, kubeSystemNamespace)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestDoNotRemoveManagedByLabelExceptForConfigManagement(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Create namespace using kubectl with managed by helm label.
	helmManagedNamespace := k8sobjects.NamespaceObject("helm-managed-namespace")
	helmManagedNamespace.Labels["app.kubernetes.io/managed-by"] = "helm"
	nt.Must(rootSyncGitRepo.Add("ns.yaml", helmManagedNamespace))
	nt.MustKubectl("apply", "-f", filepath.Join(rootSyncGitRepo.Root, "ns.yaml"))
	nt.Must(rootSyncGitRepo.Remove("ns.yaml"))

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Test that the namespace exists with managed by helm label.
	err := nt.Validate(helmManagedNamespace.Name, "", &corev1.Namespace{},
		testpredicates.HasLabel("app.kubernetes.io/managed-by", "helm"),
	)
	if err != nil {
		nt.T.Error(err)
	}

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync:        nomostest.RootSyncNN(configsync.RootSyncName),
		ObjectCount: 0, // test Namespace not committed
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestDeclareImplicitNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	var unixMilliseconds = time.Now().UnixNano() / 1000000
	var implicitNamespace = "shipping-" + fmt.Sprint(unixMilliseconds)

	err := nt.ValidateNotFound(implicitNamespace, "", &corev1.Namespace{})
	if err != nil {
		// Failed test precondition. We want to ensure we create the Namespace.
		nt.T.Fatal(err)
	}

	// Phase 1: Declare a Role in a Namespace that doesn't exist, and ensure it
	// gets created.
	roleObj := k8sobjects.RoleObject(core.Name("admin"), core.Namespace(implicitNamespace))
	nt.Must(rootSyncGitRepo.Add("acme/role.yaml", roleObj))
	nt.Must(rootSyncGitRepo.CommitAndPush("add Role in implicit Namespace " + implicitNamespace))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate(implicitNamespace, "", &corev1.Namespace{}, testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion))
	if err != nil {
		// No need to continue test since Namespace was never created.
		nt.T.Fatal(err)
	}
	err = nt.Validate("admin", implicitNamespace, &rbacv1.Role{})
	if err != nil {
		nt.T.Error(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, k8sobjects.NamespaceObject(implicitNamespace)) // implicit
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, roleObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Phase 2: Remove the Role, and ensure the implicit Namespace is NOT deleted.
	nt.Must(rootSyncGitRepo.Remove("acme/role.yaml"))
	nt.Must(rootSyncGitRepo.CommitAndPush("remove Role"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate(implicitNamespace, "", &corev1.Namespace{}, testpredicates.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion))
	if err != nil {
		nt.T.Error(err)
	}
	err = nt.ValidateNotFound("admin", implicitNamespace, &rbacv1.Role{})
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, k8sobjects.NamespaceObject(implicitNamespace)) // abandoned
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, roleObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
		// Adjust operations for this edge case.
		// The implicit object is NOT in the declared objects but it does get
		// updated to remove config sync annotations and labels.
		Operations: []metrics.ObjectOperation{
			{Operation: metrics.UpdateOperation, Count: 1}, // Namespace
		},
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestDontDeleteAllNamespaces(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	// Test Setup + Preconditions.
	// Declare two Namespaces.
	fooNS := k8sobjects.NamespaceObject("foo")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/foo/ns.yaml", k8sobjects.NamespaceObject("foo")))
	barNS := k8sobjects.NamespaceObject("bar")
	nt.Must(rootSyncGitRepo.Add("acme/namespaces/bar/ns.yaml", k8sobjects.NamespaceObject("bar")))
	nt.Must(rootSyncGitRepo.CommitAndPush("declare multiple Namespaces"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err := nt.Validate(fooNS.Name, fooNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(barNS.Name, barNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, fooNS)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, barNS)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the all declared Namespaces.
	// We expect this to fail.
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/foo/ns.yaml"))
	nt.Must(rootSyncGitRepo.Remove("acme/namespaces/bar/ns.yaml"))
	nt.Must(rootSyncGitRepo.RemoveSafetyNamespace())
	nt.Must(rootSyncGitRepo.CommitAndPush("undeclare all Namespaces"))

	require.NoError(nt.T,
		nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace, []testpredicates.Predicate{
			testpredicates.RootSyncHasSyncError(status.EmptySourceErrorCode, ""),
		}))

	// Wait 10 seconds before checking the namespaces.
	// Checking the namespaces immediately may not catch the case where
	// Config Sync deletes the namespaces even if EmptySourceError is detected.
	// TODO: Is this a bug? Why are we allowing premature deletion?
	time.Sleep(10 * time.Second)

	err = nt.Validate(fooNS.Name, fooNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(barNS.Name, barNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// While we don't expect the Namespaces to be deleted,
	// we do expect them to have been removed from the declared_resources metric.
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, fooNS)
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, barNS)
	safetyNSObj := k8sobjects.NamespaceObject(rootSyncGitRepo.SafetyNSName)
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, safetyNSObj)

	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := rootSyncGitRepo.MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerSyncError(nt, rootSyncLabels, commitHash),
		nomostest.ReconcilerSourceMetrics(nt, rootSyncLabels, commitHash,
			nt.MetricsExpectations.ExpectedRootSyncObjectCount(configsync.RootSyncName)),
		nomostest.ReconcilerOperationsMetrics(nt, rootSyncLabels,
			nt.MetricsExpectations.ExpectedRootSyncObjectOperations(configsync.RootSyncName)...),
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Sync: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Add safety back so we resume syncing.
	safetyNs := rootSyncGitRepo.SafetyNSName
	nt.Must(rootSyncGitRepo.AddSafetyNamespace())
	nt.Must(rootSyncGitRepo.CommitAndPush("re-declare safety Namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Validate(safetyNs, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its  finalizer will block until all objects in that namespace are
	// deleted.
	err = nt.Watcher.WatchForNotFound(kinds.Namespace(), barNS.Name, barNS.Namespace)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the foo & bar namespaces, now that the safety namespace is re-added
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, fooNS)
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, barNS)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, safetyNSObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Undeclare safety. We expect this to succeed since the user unambiguously wants
	// all Namespaces to be removed.
	nt.Must(rootSyncGitRepo.Remove(rootSyncGitRepo.SafetyNSPath))
	nt.Must(rootSyncGitRepo.CommitAndPush("undeclare safety Namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its  finalizer will block until all objects in that namespace are
	// deleted.
	err = nt.Watcher.WatchForNotFound(kinds.Namespace(), safetyNs, "")
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.ValidateNotFound(barNS.Name, barNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Delete the safety namespace, now that it's the last namespace
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, fooNS)
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, barNS)
	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, safetyNSObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// checkpointProtectedNamespace stores the current state of the specified
// namespace and registers a test Cleanup to restore it.
func checkpointProtectedNamespace(nt *nomostest.NT, namespace string) {
	nsObj := k8sobjects.NamespaceObject(namespace)

	if err := nt.KubeClient.Get(nsObj.Name, "", nsObj); err != nil {
		if apierrors.IsNotFound(err) {
			nt.T.Cleanup(func() {
				// Revert to initial state (not found).
				if err := nomostest.DeleteObjectsAndWait(nt, nsObj); err != nil {
					nt.T.Fatal(err)
				}
			})
			return
		}
		nt.T.Fatalf("Failed to get %q namespace: %v", namespace, err)
	}
	// Remove unique identifiers (reset may happen after update or delete)
	nsObj.SetUID("")
	nsObj.SetResourceVersion("")
	nsObj.SetGeneration(0)
	// Remove managed fields (Config Sync doesn't allow them in the source of truth)
	nsObj.SetManagedFields(nil)

	nt.T.Cleanup(func() {
		// Revert to initial state.
		// Removes the test label, which avoids triggering deletion by Reset/Clean.
		if err := nt.KubeClient.Update(nsObj); err != nil {
			if apierrors.IsNotFound(err) {
				if err := nt.KubeClient.Create(nsObj); err != nil {
					nt.T.Errorf("Failed to revert %q namespace: %v", namespace, err)
				}
			} else {
				nt.T.Errorf("Failed to revert %q namespace: %v", namespace, err)
			}
		}
	})
}
