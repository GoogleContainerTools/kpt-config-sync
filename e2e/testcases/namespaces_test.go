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

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestDeclareNamespace runs a test that ensures ACM syncs Namespaces to clusters.
func TestDeclareNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	err := nt.ValidateNotFound("foo", "", &corev1.Namespace{})
	if err != nil {
		// Failed test precondition.
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add Namespace")
	nt.WaitForRepoSyncs()

	// Test that the Namespace "foo" exists.
	err = nt.Validate("foo", "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate no error metrics are emitted.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourceCreated("Namespace"))
		if err != nil {
			return err
		}

		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestNamespaceLabelAndAnnotationLifecycle(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	// Create foo namespace without any labels or annotations.
	fooNamespace := fake.NamespaceObject("foo")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fooNamespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Create foo namespace")
	nt.WaitForRepoSyncs()

	// Test that the namespace exists.
	err := nt.Validate(fooNamespace.Name, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourceCreated("Namespace"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Add label and annotation to namespace.
	fooNamespace.Labels["label"] = "test-label"
	fooNamespace.Annotations["annotation"] = "test-annotation"
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fooNamespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Updated foo namespace to include label and annotation")
	nt.WaitForRepoSyncs()

	// Test that the namespace exists with label and annotation.
	err = nt.Validate(fooNamespace.Name, "", &corev1.Namespace{}, nomostest.HasLabel("label", "test-label"), nomostest.HasAnnotation("annotation", "test-annotation"))
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourcePatched("Namespace", 1))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Update label and annotation to namespace.
	fooNamespace.Labels["label"] = "updated-test-label"
	fooNamespace.Annotations["annotation"] = "updated-test-annotation"
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fooNamespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Updated foo namespace to include label and annotation")
	nt.WaitForRepoSyncs()

	// Test that the namespace exists with the updated label and annotation.
	err = nt.Validate(fooNamespace.Name, "", &corev1.Namespace{}, nomostest.HasLabel("label", "updated-test-label"), nomostest.HasAnnotation("annotation", "updated-test-annotation"))
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourcePatched("Namespace", 1))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove label and annotation to namespace and commit.
	delete(fooNamespace.Labels, "label")
	delete(fooNamespace.Annotations, "annotation")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fooNamespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Updated foo namespace, removing label and annotation")
	nt.WaitForRepoSyncs()

	// Test that the namespace exists without the label and annotation.
	err = nt.Validate(fooNamespace.Name, "", &corev1.Namespace{}, nomostest.MissingLabel("label"), nomostest.MissingAnnotation("annotation"))
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourcePatched("Namespace", 1))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestNamespaceExistsAndDeclared(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	// Create namespace using kubectl first then commit.
	namespace := fake.NamespaceObject("decl-namespace-annotation-none")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/decl-namespace-annotation-none/ns.yaml", namespace)
	nt.MustKubectl("apply", "-f", filepath.Join(nt.RootRepos[configsync.RootSyncName].Root, "acme/namespaces/decl-namespace-annotation-none/ns.yaml"))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add namespace")

	nt.WaitForRepoSyncs()

	// Test that the namespace exists after sync.
	err := nt.Validate(namespace.Name, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourceCreated("Namespace"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestNamespaceEnabledAnnotationNotDeclared(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	// Create namespace with managed annotation using kubectl.
	namespace := fake.NamespaceObject("undeclared-annotation-enabled")
	namespace.Annotations["configmanagement.gke.io/managed"] = "enabled"
	nt.RootRepos[configsync.RootSyncName].Add("ns.yaml", namespace)
	nt.MustKubectl("apply", "-f", filepath.Join(nt.RootRepos[configsync.RootSyncName].Root, "ns.yaml"))
	nt.RootRepos[configsync.RootSyncName].Remove("ns.yaml")

	nt.WaitForRepoSyncs()

	// Test that the namespace exists after sync.
	err := nt.Validate(namespace.Name, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount(), // test Namespace not committed
			metrics.ResourceCreated("Namespace"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

// TestManagementDisabledNamespace tests https://cloud.google.com/anthos-config-management/docs/how-to/managing-objects#unmanaged-namespaces.
func TestManagementDisabledNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	checkpointProtectedNamespace(nt, metav1.NamespaceDefault)

	namespacesToTest := []string{"foo", metav1.NamespaceDefault}
	for _, nsName := range namespacesToTest {
		// Create namespace.
		namespace := fake.NamespaceObject(nsName)
		cm1 := fake.ConfigMapObject(core.Namespace(nsName), core.Name("cm1"))
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", nsName), namespace)
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/cm1.yaml", nsName), cm1)
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Create a namespace and a configmap")
		nt.WaitForRepoSyncs()

		// Test that the namespace exists with expected config management labels and annotations.
		err := nt.Validate(namespace.Name, "", &corev1.Namespace{}, nomostest.HasAllNomosMetadata())
		if err != nil {
			nt.T.Error(err)
		}

		// Test that the configmap exists with expected config management labels and annotations.
		err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, nomostest.HasAllNomosMetadata())
		if err != nil {
			nt.T.Error(err)
		}

		// Validate multi-repo metrics.
		err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
			err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
				nt.DefaultRootSyncObjectCount()+2, // 2 for the test Namespace & ConfigMap
				metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("ConfigMap"))
			if err != nil {
				return err
			}
			return nt.ValidateErrorMetricsNotFound()
		})
		if err != nil {
			nt.T.Error(err)
		}

		// Update the namespace and the configmap to be no longer be managed
		namespace.Annotations[metadata.ResourceManagementKey] = metadata.ResourceManagementDisabled
		cm1.Annotations[metadata.ResourceManagementKey] = metadata.ResourceManagementDisabled
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/ns.yaml", nsName), namespace)
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/namespaces/%s/cm1.yaml", nsName), cm1)
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Unmanage the namespace and the configmap")
		nt.WaitForRepoSyncs()

		// Test that the now unmanaged namespace does not contain any config management labels or annotations
		err = nt.Validate(namespace.Name, "", &corev1.Namespace{}, nomostest.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Error(err)
		}

		// Test that the now unmanaged configmap does not contain any config management labels or annotations
		err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, nomostest.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Error(err)
		}

		// Validate multi-repo metrics.
		err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
			err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
				nt.DefaultRootSyncObjectCount()+2, // 2 for the test Namespace & ConfigMap
				metrics.ResourcePatched("Namespace", 1), metrics.ResourcePatched("ConfigMap", 1))
			if err != nil {
				return err
			}
			return nt.ValidateErrorMetricsNotFound()
		})
		if err != nil {
			nt.T.Error(err)
		}

		// Remove the namspace and the configmap from the repository
		nt.RootRepos[configsync.RootSyncName].Remove(fmt.Sprintf("acme/namespaces/%s", nsName))
		nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove the namespace and the configmap")
		nt.WaitForRepoSyncs()

		// Test that the namespace still exists on the cluster, and does not contain any config management labels or annotations
		err = nt.Validate(namespace.Name, "", &corev1.Namespace{}, nomostest.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Error(err)
		}

		// Test that the configmap still exists on the cluster, and does not contain any config management labels or annotations
		err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, nomostest.NoConfigSyncMetadata())
		if err != nil {
			nt.T.Error(err)
		}

		// Validate multi-repo metrics.
		err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
			err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
				nt.DefaultRootSyncObjectCount())
			if err != nil {
				return err
			}
			return nt.ValidateErrorMetricsNotFound()
		})
		if err != nil {
			nt.T.Error(err)
		}
	}
}

// TestManagementDisabledConfigMap tests https://cloud.google.com/anthos-config-management/docs/how-to/managing-objects#stop-managing.
func TestManagementDisabledConfigMap(t *testing.T) {
	fooNamespace := fake.NamespaceObject("foo")
	cm1 := fake.ConfigMapObject(core.Namespace("foo"), core.Name("cm1"))
	// Initialize repo with disabled resource to test initial sync w/ unmanaged resources
	cm2 := fake.ConfigMapObject(core.Namespace("foo"), core.Name("cm2"), core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementDisabled))
	cm3 := fake.ConfigMapObject(core.Namespace("foo"), core.Name("cm3"))

	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.WithInitialCommit(ntopts.Commit{
		Message: "Create namespace and configmaps",
		Files: map[string]client.Object{
			"acme/namespaces/foo/ns.yaml":  fooNamespace,
			"acme/namespaces/foo/cm1.yaml": cm1,
			"acme/namespaces/foo/cm2.yaml": cm2,
			"acme/namespaces/foo/cm3.yaml": cm3,
		},
	}))

	// Test that the namespace exists with expected config management labels and annotations.
	err := nt.Validate(fooNamespace.Name, "", &corev1.Namespace{}, nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Test that cm1 exists with expected config management labels and annotations.
	err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Test that the unmanaged cm2 does not exist.
	err = nt.ValidateNotFound(cm2.Name, cm2.Namespace, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Error(err)
	}

	// Test that cm3 exists with expected config management labels and annotations.
	err = nt.Validate(cm3.Name, cm3.Namespace, &corev1.ConfigMap{}, nomostest.HasAllNomosMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+4, // 4 for the test Namespace & ConfigMaps
			metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("ConfigMap"))
		if err != nil {
			return err
		}

		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Update the configmap to be no longer be managed
	cm1.Annotations[metadata.ResourceManagementKey] = metadata.ResourceManagementDisabled
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/cm1.yaml", cm1)
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/cm3.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Unmanage cm1 and remove cm3")
	nt.WaitForRepoSyncs()

	// Test that the now unmanaged configmap does not contain any config management labels or annotations
	err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, nomostest.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Test that cm3 was properly pruned.
	err = nt.ValidateNotFound(cm3.Name, cm3.Namespace, &corev1.ConfigMap{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+3, // 3 for the test Namespace & ConfigMaps
			metrics.ResourcePatched("ConfigMap", 1))
		if err != nil {
			return err
		}

		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove the configmap from the repository
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/cm1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove the configmap")
	nt.WaitForRepoSyncs()

	// Test that the configmap still exists on the cluster, and does not contain any config management labels or annotations
	err = nt.Validate(cm1.Name, cm1.Namespace, &corev1.ConfigMap{}, nomostest.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+2) // 2 for the test Namespace & ConfigMap
		if err != nil {
			return err
		}

		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestSyncLabelsAndAnnotationsOnKubeSystem(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.SkipAutopilotCluster)

	checkpointProtectedNamespace(nt, metav1.NamespaceSystem)

	// Update kube-system namespace to be managed.
	kubeSystemNamespace := fake.NamespaceObject(metav1.NamespaceSystem)
	kubeSystemNamespace.Labels["test-corp.com/awesome-controller-flavour"] = "fuzzy"
	kubeSystemNamespace.Annotations["test-corp.com/awesome-controller-mixin"] = "green"
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/kube-system/ns.yaml", kubeSystemNamespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add namespace")
	nt.WaitForRepoSyncs()

	// Test that the kube-system namespace exists with label and annotation.
	err := nt.Validate(kubeSystemNamespace.Name, "", &corev1.Namespace{},
		nomostest.HasLabel("test-corp.com/awesome-controller-flavour", "fuzzy"),
		nomostest.HasAnnotation("test-corp.com/awesome-controller-mixin", "green"),
	)
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourceCreated("Namespace"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove label and annotation from the kube-system namespace.
	delete(kubeSystemNamespace.Labels, "test-corp.com/awesome-controller-flavour")
	delete(kubeSystemNamespace.Annotations, "test-corp.com/awesome-controller-mixin")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/kube-system/ns.yaml", kubeSystemNamespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Remove label and annotation")
	nt.WaitForRepoSyncs()

	// Test that the kube-system namespace exists without the label and annotation.
	err = nt.Validate(kubeSystemNamespace.Name, "", &corev1.Namespace{},
		nomostest.MissingLabel("test-corp.com/awesome-controller-flavour"), nomostest.MissingAnnotation("test-corp.com/awesome-controller-mixin"))
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourcePatched("Namespace", 1))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Update kube-system namespace to be no longer be managed.
	kubeSystemNamespace.Annotations["configmanagement.gke.io/managed"] = "disabled"
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/kube-system/ns.yaml", kubeSystemNamespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Update namespace to no longer be managed")
	nt.WaitForRepoSyncs()

	// Test that the now unmanaged kube-system namespace does not contain any config management labels or annotations.
	err = nt.Validate(kubeSystemNamespace.Name, "", &corev1.Namespace{}, nomostest.NoConfigSyncMetadata())
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+1, // 1 for the test Namespace
			metrics.ResourcePatched("Namespace", 1))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestDoNotRemoveManagedByLabelExceptForConfigManagement(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	// Create namespace using kubectl with managed by helm label.
	helmManagedNamespace := fake.NamespaceObject("helm-managed-namespace")
	helmManagedNamespace.Labels["app.kubernetes.io/managed-by"] = "helm"
	nt.RootRepos[configsync.RootSyncName].Add("ns.yaml", helmManagedNamespace)
	nt.MustKubectl("apply", "-f", filepath.Join(nt.RootRepos[configsync.RootSyncName].Root, "ns.yaml"))
	nt.RootRepos[configsync.RootSyncName].Remove("ns.yaml")

	nt.WaitForRepoSyncs()

	// Test that the namespace exists with managed by helm label.
	err := nt.Validate(helmManagedNamespace.Name, "", &corev1.Namespace{},
		nomostest.HasLabel("app.kubernetes.io/managed-by", "helm"),
	)
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount(), // test Namespace not committed
			metrics.ResourceCreated("Namespace"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestDeclareImplicitNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2, ntopts.Unstructured)

	var unixMilliseconds = time.Now().UnixNano() / 1000000
	var implicitNamespace = "shipping-" + fmt.Sprint(unixMilliseconds)

	err := nt.ValidateNotFound(implicitNamespace, "", &corev1.Namespace{})
	if err != nil {
		// Failed test precondition. We want to ensure we create the Namespace.
		nt.T.Fatal(err)
	}

	// Phase 1: Declare a Role in a Namespace that doesn't exist, and ensure it
	// gets created.
	nt.RootRepos[configsync.RootSyncName].Add("acme/role.yaml", fake.RoleObject(core.Name("admin"), core.Namespace(implicitNamespace)))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("add Role in implicit Namespace " + implicitNamespace)
	nt.WaitForRepoSyncs()

	err = nt.Validate(implicitNamespace, "", &corev1.Namespace{}, nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion))
	if err != nil {
		// No need to continue test since Namespace was never created.
		nt.T.Fatal(err)
	}
	err = nt.Validate("admin", implicitNamespace, &rbacv1.Role{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount()+2, // 2 for the test Namespace & Role
			metrics.ResourceCreated("Namespace"), metrics.ResourceCreated("Role"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Phase 2: Remove the Role, and ensure the implicit Namespace is NOT deleted.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/role.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("remove Role")
	nt.WaitForRepoSyncs()

	err = nt.Validate(implicitNamespace, "", &corev1.Namespace{}, nomostest.HasAnnotation(common.LifecycleDeleteAnnotation, common.PreventDeletion))
	if err != nil {
		nt.T.Error(err)
	}
	err = nt.ValidateNotFound("admin", implicitNamespace, &rbacv1.Role{})
	if err != nil {
		nt.T.Error(err)
	}

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			nt.DefaultRootSyncObjectCount(),
			metrics.ResourceDeleted("Role"))
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

func TestDontDeleteAllNamespaces(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation2)

	// Test Setup + Preconditions.
	// Declare two Namespaces.
	fooNS := fake.NamespaceObject("foo")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	barNS := fake.NamespaceObject("bar")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/bar/ns.yaml", fake.NamespaceObject("bar"))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("declare multiple Namespaces")
	nt.WaitForRepoSyncs()

	err := nt.Validate(fooNS.Name, fooNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.Validate(barNS.Name, barNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	numDeclaredObjs := nt.DefaultRootSyncObjectCount()
	numDeclaredObjs += 2 // 2 for the test Namespaces

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			numDeclaredObjs,
			metrics.GVKMetric{
				GVK:   "Namespace",
				APIOp: "update",
				ApplyOps: []metrics.Operation{
					{Name: "update", Count: 2},
				},
				Watches: "1",
			})
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Remove the all declared Namespaces.
	// We expect this to fail.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/ns.yaml")
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/bar/ns.yaml")
	nt.RootRepos[configsync.RootSyncName].RemoveSafetyNamespace()
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("undeclare all Namespaces")

	require.NoError(nt.T,
		nomostest.WatchObject(nt, kinds.RootSyncV1Beta1(), configsync.RootSyncName, configsync.ControllerNamespace, []nomostest.Predicate{
			nomostest.RootSyncHasSyncError(nt, status.EmptySourceErrorCode, ""),
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
	numDeclaredObjs -= 2 // -2 for the removed test Namespaces
	numDeclaredObjs--    // -1 for the removed safety Namespace

	err = nt.ValidateMetrics(nomostest.SyncMetricsToReconcilerSyncError(nt, nomostest.DefaultRootReconcilerName), func() error {
		rootReconcilerMetrics := nt.ReconcilerMetrics.FilterByReconciler(nomostest.DefaultRootReconcilerName)
		err := rootReconcilerMetrics.ValidateDeclaredResources(numDeclaredObjs)
		return errors.Wrapf(err, "for reconciler %s", nomostest.DefaultRootReconcilerName)
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Add safety back so we resume syncing.
	safetyNs := nt.RootRepos[configsync.RootSyncName].SafetyNSName
	nt.RootRepos[configsync.RootSyncName].AddSafetyNamespace()
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("re-declare safety Namespace")
	nt.WaitForRepoSyncs()

	err = nt.Validate(safetyNs, "", &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}
	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its  finalizer will block until all objects in that namespace are
	// deleted.
	err = nomostest.WatchForNotFound(nt, kinds.Namespace(), barNS.Name, barNS.Namespace)
	if err != nil {
		nt.T.Fatal(err)
	}

	numDeclaredObjs++ // 1 for the re-added safety Namespace

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			numDeclaredObjs,
			metrics.ResourceCreated("Namespace"),
			metrics.GVKMetric{
				GVK:      "Namespace",
				APIOp:    "",
				ApplyOps: []metrics.Operation{{Name: "update", Count: 4}},
				Watches:  "1",
			},
			metrics.GVKMetric{
				GVK:      "Namespace",
				APIOp:    "delete",
				ApplyOps: []metrics.Operation{{Name: "delete", Count: 1}},
				Watches:  "1",
			})
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}

	// Undeclare safety. We expect this to succeed since the user unambiguously wants
	// all Namespaces to be removed.
	nt.RootRepos[configsync.RootSyncName].Remove(nt.RootRepos[configsync.RootSyncName].SafetyNSPath)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("undeclare safety Namespace")
	nt.WaitForRepoSyncs()

	// Namespace should be marked as deleted, but may not be NotFound yet,
	// because its  finalizer will block until all objects in that namespace are
	// deleted.
	err = nomostest.WatchForNotFound(nt, kinds.Namespace(), safetyNs, "")
	if err != nil {
		nt.T.Fatal(err)
	}
	err = nt.ValidateNotFound(barNS.Name, barNS.Namespace, &corev1.Namespace{})
	if err != nil {
		nt.T.Fatal(err)
	}

	numDeclaredObjs-- // -1 for the removed safety Namespace

	// Validate multi-repo metrics.
	err = nt.ValidateMetrics(nomostest.SyncMetricsToLatestCommit(nt), func() error {
		err := nt.ValidateMultiRepoMetrics(nomostest.DefaultRootReconcilerName,
			numDeclaredObjs,
			metrics.GVKMetric{
				GVK:      "Namespace",
				APIOp:    "delete",
				ApplyOps: []metrics.Operation{{Name: "delete", Count: 2}},
				Watches:  "0",
			})
		if err != nil {
			return err
		}
		return nt.ValidateErrorMetricsNotFound()
	})
	if err != nil {
		nt.T.Error(err)
	}
}

// checkpointProtectedNamespace stores the current state of the specified
// namespace and registers a test Cleanup to restore it.
func checkpointProtectedNamespace(nt *nomostest.NT, namespace string) {
	nsObj := fake.NamespaceObject(namespace)

	if err := nt.Get(nsObj.Name, "", nsObj); err != nil {
		if apierrors.IsNotFound(err) {
			nt.T.Cleanup(func() {
				// Revert to initial state (not found).
				if err := nt.Delete(nsObj); err != nil {
					if !apierrors.IsNotFound(err) {
						nt.T.Errorf("Failed to revert %q namespace: %v", namespace, err)
					}
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
		if err := nt.Update(nsObj); err != nil {
			if apierrors.IsNotFound(err) {
				if err := nt.Create(nsObj); err != nil {
					nt.T.Errorf("Failed to revert %q namespace: %v", namespace, err)
				}
			} else {
				nt.T.Errorf("Failed to revert %q namespace: %v", namespace, err)
			}
		}
	})
}
