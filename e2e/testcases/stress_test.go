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
	"reflect"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/iam"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	"kpt.dev/configsync/e2e/nomostest/syncsource"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testresourcegroup"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/util/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var crontabGVK = schema.GroupVersionKind{
	Group:   "stable.example.com",
	Kind:    "CronTab",
	Version: "v1",
}

func crontabCR(namespace, name string) (*unstructured.Unstructured, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(crontabGVK)
	u.SetName(name)
	u.SetNamespace(namespace)
	err := unstructured.SetNestedField(u.Object, "* * * * */5", "spec", "cronSpec")
	return u, err
}

// TestStressCRD tests Config Sync can sync one CRD and 1000 namespaces successfully.
// Every namespace includes a ConfigMap and a CR.
func TestStressCRD(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.StressTest,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	syncPath := gitproviders.DefaultSyncDir

	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)
	nt.Must(nt.WatchForAllSyncs())

	crdName := "crontabs.stable.example.com"
	nt.T.Logf("Delete the %q CRD if needed", crdName)
	nt.MustKubectl("delete", "crd", crdName, "--ignore-not-found")

	crdContent, err := os.ReadFile("../testdata/customresources/changed_schema_crds/old_schema_crd.yaml")
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.Must(rootSyncGitRepo.AddFile(fmt.Sprintf("%s/crontab-crd.yaml", syncPath), crdContent))

	labelKey := "StressTestName"
	labelValue := "TestStressCRD"
	for i := 1; i <= 1000; i++ {
		ns := fmt.Sprintf("foo%d", i)
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/ns-%d.yaml", syncPath, i), k8sobjects.NamespaceObject(
			ns, core.Label(labelKey, labelValue))))
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/cm-%d.yaml", syncPath, i), k8sobjects.ConfigMapObject(
			core.Name("cm1"), core.Namespace(ns), core.Label(labelKey, labelValue))))

		cr, err := crontabCR(ns, "cr1")
		if err != nil {
			nt.T.Fatal(err)
		}
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/crontab-cr-%d.yaml", syncPath, i), cr))
	}
	nt.Must(rootSyncGitRepo.CommitAndPush("Add configs (one CRD and 1000 Namespaces (every namespace has one ConfigMap and one CR)"))
	nt.Must(nt.WatchForAllSyncs(nomostest.WithTimeout(30 * time.Minute)))

	nt.T.Logf("Verify that the CronTab CRD is installed on the cluster")
	if err := nt.Validate(crdName, "", k8sobjects.CustomResourceDefinitionV1Object()); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify that there are exactly 1000 Namespaces managed by Config Sync on the cluster")
	nt.Must(validateNumberOfObjectsEquals(nt, kinds.Namespace(), 1000,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue}))

	nt.T.Logf("Verify that there are exactly 1000 ConfigMaps managed by Config Sync on the cluster")
	nt.Must(validateNumberOfObjectsEquals(nt, kinds.ConfigMap(), 1000,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue}))

	nt.T.Logf("Verify that there are exactly 1000 CronTab CRs managed by Config Sync on the cluster")
	nt.Must(validateNumberOfObjectsEquals(nt, crontabGVK, 1000,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue}))
}

// TestStressLargeNamespace tests that Config Sync can sync a namespace including 5000 resources successfully.
func TestStressLargeNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.StressTest,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	syncPath := gitproviders.DefaultSyncDir
	ns := "my-ns-1"

	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)

	nt.T.Log("Override the memory limit of the reconciler container of root-reconciler to 800MiB")
	rootSync := k8sobjects.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "reconciler", "memoryLimit": "800Mi"}]}}}`)
	nt.Must(nt.WatchForAllSyncs())

	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), k8sobjects.NamespaceObject(ns)))

	labelKey := "StressTestName"
	labelValue := "TestStressLargeNamespace"
	for i := 1; i <= 5000; i++ {
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/cm-%d.yaml", syncPath, i), k8sobjects.ConfigMapObject(
			core.Name(fmt.Sprintf("cm-%d", i)), core.Namespace(ns), core.Label(labelKey, labelValue))))
	}
	nt.Must(rootSyncGitRepo.CommitAndPush("Add configs (5000 ConfigMaps and 1 Namespace"))
	nt.Must(nt.WatchForAllSyncs(nomostest.WithTimeout(10 * time.Minute)))

	nt.T.Log("Verify there are 5000 ConfigMaps in the namespace")
	nt.Must(validateNumberOfObjectsEquals(nt, kinds.ConfigMap(), 5000,
		client.MatchingLabels{labelKey: labelValue},
		client.InNamespace(ns)))
}

// TestStressFrequentGitCommits adds 100 Git commits, and verifies that Config Sync can sync the changes in these commits successfully.
func TestStressFrequentGitCommits(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.StressTest,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	syncPath := gitproviders.DefaultSyncDir
	ns := "bookstore"

	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)

	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), k8sobjects.NamespaceObject(ns)))
	nt.Must(rootSyncGitRepo.CommitAndPush(fmt.Sprintf("add a namespace: %s", ns)))
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Logf("Add 100 commits (every commit adds a new ConfigMap object into the %s namespace)", ns)
	labelKey := "StressTestName"
	labelValue := "TestStressFrequentGitCommits"
	for i := 0; i < 100; i++ {
		cmName := fmt.Sprintf("cm-%v", i)
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/%s.yaml", syncPath, cmName),
			k8sobjects.ConfigMapObject(core.Name(cmName), core.Namespace(ns), core.Label(labelKey, labelValue))))
		nt.Must(rootSyncGitRepo.CommitAndPush(fmt.Sprintf("add %s", cmName)))
	}
	nt.Must(nt.WatchForAllSyncs(nomostest.WithTimeout(10 * time.Minute)))

	nt.T.Logf("Verify that there are exactly 100 ConfigMaps under the %s namespace", ns)
	nt.Must(validateNumberOfObjectsEquals(nt, kinds.ConfigMap(), 100,
		client.MatchingLabels{labelKey: labelValue},
		client.InNamespace(ns)))
}

// This test creates a RootSync pointed at https://github.com/config-sync-examples/crontab-crs
// This repository contains 13,000+ objects, which takes a long time to reconcile
func TestStressLargeRequest(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.StressTest,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	crdName := "crontabs.stable.example.com"

	oldCrdFilePath := "../testdata/customresources/changed_schema_crds/old_schema_crd.yaml"
	nt.T.Logf("Apply the old CronTab CRD defined in %s", oldCrdFilePath)
	nt.MustKubectl("apply", "-f", oldCrdFilePath)

	nt.T.Logf("Wait until the old CRD is established")
	nt.Must(nt.Watcher.WatchObject(kinds.CustomResourceDefinitionV1(), crdName, "",
		testwatcher.WatchPredicates(nomostest.IsEstablished)))

	reconcilerOverride := v1beta1.ContainerResourcesSpec{
		ContainerName: reconcilermanager.Reconciler,
		MemoryLimit:   resource.MustParse("1500Mi"),
	}
	if *e2e.GKEAutopilot {
		reconcilerOverride.MemoryRequest = resource.MustParse("1500Mi")
	}
	repo := gitproviders.ReadOnlyRepository{
		URL: "https://github.com/config-sync-examples/crontab-crs",
	}
	rootSync := nt.RootSyncObjectGit(rootSyncID.Name, repo, gitproviders.MainBranch, "", "configs", configsync.SourceFormatUnstructured)
	rootSync.Spec.Git.Auth = configsync.AuthNone
	rootSync.Spec.SecretRef = nil
	rootSync.Spec.SafeOverride().OverrideSpec.StatusMode = metadata.StatusDisabled.String()
	rootSync.Spec.SafeOverride().OverrideSpec.Resources = []v1beta1.ContainerResourcesSpec{reconcilerOverride}
	nt.T.Logf("Apply the RootSync object to sync to %s", rootSync.Spec.Git.Repo)
	nt.Must(nt.KubeClient.Apply(rootSync))

	nt.T.Logf("Verify that the source errors are truncated")
	nt.Must(nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), rootSyncID.Name, rootSyncID.Namespace,
		testwatcher.WatchPredicates(truncateSourceErrors())))

	newCrdFilePath := "../testdata/customresources/changed_schema_crds/new_schema_crd.yaml"
	nt.T.Logf("Apply the new CronTab CRD defined in %s", newCrdFilePath)
	nt.MustKubectl("apply", "-f", newCrdFilePath)

	nt.T.Logf("Wait until the new CRD is established")
	nt.Must(nt.Watcher.WatchObject(kinds.CustomResourceDefinitionV1(), crdName, "",
		testwatcher.WatchPredicates(nomostest.IsEstablished)))

	commit, err := nomostest.GitCommitFromSpec(nt, rootSync.Spec.Git)
	if err != nil {
		nt.T.Fatal(err)
	}
	nomostest.SetExpectedGitCommit(nt, rootSyncID, commit)

	nt.T.Logf("Wait for the sync to complete")
	nt.Must(nt.WatchForAllSyncs())
}

// TestStress100CRDs applies 100 CRDs and validates that syncing still works.
// This simulates a scenario similar to using Config Connector or Crossplane.
// This test also validates that nomos status does not use client-side throttling.
func TestStress100CRDs(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.StressTest,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	syncPath := gitproviders.DefaultSyncDir
	ns := "stress-test-ns"
	crdCount := 100

	nt.T.Cleanup(func() {
		for i := 1; i <= crdCount; i++ {
			crd := fakeCRD("Anvil", fmt.Sprintf("acme-%d.com", i))
			if err := nt.KubeClient.Delete(crd); err != nil {
				if !apierrors.IsNotFound(err) {
					nt.T.Error(err)
				}
			}
		}
	})

	for i := 1; i <= crdCount; i++ {
		crd := fakeCRD("Anvil", fmt.Sprintf("acme-%d.com", i))
		if err := nt.KubeClient.Create(crd); err != nil {
			nt.T.Fatal(err)
		}
	}

	nt.T.Log("Replacing the reconciler Pod to ensure the new CRDs are discovered")
	nomostest.DeletePodByLabel(nt, "app", reconcilermanager.Reconciler, true)

	nt.T.Log("Adding a test namespace to the repo to trigger a new sync")
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), k8sobjects.NamespaceObject(ns)))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add a test namespace to trigger a new sync"))

	nt.T.Logf("Waiting for the sync to complete")
	nt.Must(nt.WatchForAllSyncs())

	// Validate client-side throttling is disabled for nomos status
	out, err := nt.Shell.ExecWithDebug("nomos", "status")
	if err != nil {
		nt.T.Fatal(err)
	}

	if strings.Contains(string(out), "due to client-side throttling") {
		nt.T.Fatalf("Nomos status should not perform client-side throttling: %s", string(out))
	}
}

// TestStressManyDeployments applies 1000 Deployments with 1 replica each.
// This ensures that Config Sync can apply resources while the cluster control
// plane is autoscaling up to meet demand. This requires cluster autoscaling to
// be enabled.
//
// This test needs at least 10 nodes on GKE, due to the 110 pods per node limit.
// See DefaultGKENumNodesForStressTests for the exact number of nodes per zone.
func TestStressManyDeployments(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.StressTest,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	syncPath := filepath.Join(gitproviders.DefaultSyncDir, "stress-test")
	ns := "stress-test-ns"

	deployCount := 1000

	nt.T.Logf("Adding a test namespace and %d deployments", deployCount)
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), k8sobjects.NamespaceObject(ns)))

	for i := 1; i <= deployCount; i++ {
		name := fmt.Sprintf("pause-%d", i)
		nt.Must(rootSyncGitRepo.Add(
			fmt.Sprintf("%s/namespaces/%s/deployment-%s.yaml", syncPath, ns, name),
			pauseDeploymentObject(nt, name, ns)))
	}

	nt.Must(rootSyncGitRepo.CommitAndPush(fmt.Sprintf("Adding a test namespace and %d deployments", deployCount)))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Logf("Verify the number of Deployment objects")
	nt.Must(validateNumberOfObjectsEquals(nt, kinds.Deployment(), deployCount,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
		client.InNamespace(ns)))

	nt.T.Log("Removing resources from Git")
	nt.Must(rootSyncGitRepo.Remove(syncPath))
	nt.Must(rootSyncGitRepo.CommitAndPush("Removing resources from Git"))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs())
}

// TestStressMemoryUsageGit applies 100 CRDs and then 50 objects for each
// resource. This stressed both the number of watches and the number of objects,
// which increases memory usage.
func TestStressMemoryUsageGit(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.StressTest,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	syncPath := filepath.Join(gitproviders.DefaultSyncDir, "stress-test")
	ns := "stress-test-ns"
	kind := "Anvil"

	crdCount := 100
	crCount := 50

	nt.T.Logf("Adding a test namespace, %d crds, and %d objects per crd", crdCount, crCount)
	nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), k8sobjects.NamespaceObject(ns)))

	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		crd := fakeCRD(kind, group)
		nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/crd-%s.yaml", syncPath, crd.Name), crd))
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v2",
		}
		for j := 1; j <= crCount; j++ {
			cr := fakeCR(fmt.Sprintf("%s-%d", strings.ToLower(kind), j), ns, gvk)
			nt.Must(rootSyncGitRepo.Add(fmt.Sprintf("%s/namespaces/%s/%s-%s.yaml", syncPath, ns, crd.Name, cr.GetName()), cr))
		}
	}

	nt.Must(rootSyncGitRepo.CommitAndPush(fmt.Sprintf("Adding a test namespace, %d crds, and %d objects per crd", crdCount, crCount)))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Logf("Verify the number of Anvil objects")
	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v2",
		}
		nt.Must(validateNumberOfObjectsEquals(nt, gvk, crCount,
			client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
			client.InNamespace(ns)))
	}

	nt.T.Log("Removing resources from Git")
	nt.Must(rootSyncGitRepo.Remove(syncPath))
	nt.Must(rootSyncGitRepo.CommitAndPush("Removing resources from Git"))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs())
}

// TestStressMemoryUsageOCI applies 100 CRDs and then 50 objects for each
// resource. This stressed both the number of watches and the number of objects,
// which increases memory usage.
//
// Requirements:
// 1. crane https://github.com/google/go-containerregistry/tree/main/cmd/crane
// 2. gcloud auth login OR gcloud auth activate-service-account ACCOUNT --key-file=KEY-FILE
// 3. Artifact Registry repo: us-docker.pkg.dev/${GCP_PROJECT}/config-sync-test-private
// 4. GKE cluster with Workload Identity
// 5. Google Service Account: e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com
// 6. IAM for the GSA to read from the Artifact Registry repo
// 7. IAM for the test runner to write to Artifact Registry repo
func TestStressMemoryUsageOCI(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.WorkloadIdentity, ntopts.StressTest,
		ntopts.RequireOCIProvider,
		ntopts.SyncWithGitSource(rootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	if err := workloadidentity.ValidateEnabled(nt); err != nil {
		nt.T.Fatal(err)
	}
	gsaEmail := registryproviders.ArtifactRegistryReaderEmail()
	if err := iam.ValidateServiceAccountExists(nt, gsaEmail); err != nil {
		nt.T.Fatal(err)
	}

	ns := "stress-test-ns"
	kind := "Anvil"

	crdCount := 100
	crCount := 50

	nt.T.Logf("Adding a test namespace, %d crds, and %d objects per crd", crdCount, crCount)
	var packageObjs []client.Object
	packageObjs = append(packageObjs, k8sobjects.NamespaceObject(ns))

	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		crd := fakeCRD(kind, group)
		packageObjs = append(packageObjs, crd)
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v2",
		}
		for j := 1; j <= crCount; j++ {
			cr := fakeCR(fmt.Sprintf("%s-%d", strings.ToLower(kind), j), ns, gvk)
			packageObjs = append(packageObjs, cr)
		}
	}

	image, err := nt.BuildAndPushOCIImage(rootSyncID.ObjectKey,
		registryproviders.ImageInputObjects(nt.Scheme, packageObjs...))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Update RootSync to sync from the OCI image in Artifact Registry")
	rs := nt.RootSyncObjectOCI(rootSyncID.Name, image.OCIImageID().WithoutDigest(), "", image.Digest)
	nt.Must(nt.KubeClient.Apply(rs))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs())

	nt.T.Logf("Verify the number of Anvil objects")
	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v2",
		}
		nt.Must(validateNumberOfObjectsEquals(nt, gvk, crCount,
			client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
			client.InNamespace(ns)))
	}

	nt.T.Log("Remove all files and publish an empty OCI image")
	emptyImage, err := nt.BuildAndPushOCIImage(rootSyncID.ObjectKey)
	if err != nil {
		nt.T.Fatal(err)
	}
	nomostest.SetExpectedOCIImageDigest(nt, rootSyncID, emptyImage.Digest)

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs())
}

// TestStressMemoryUsageHelm applies 100 CRDs and then 50 objects for each
// resource. This stresses both the number of watches and the number of objects,
// which increases memory usage.
//
// Requirements:
// 1. Helm auth https://cloud.google.com/artifact-registry/docs/helm/authentication
// 2. gcloud auth login OR gcloud auth activate-service-account ACCOUNT --key-file=KEY-FILE
// 3. Artifact Registry repo: us-docker.pkg.dev/${GCP_PROJECT}/config-sync-test-private
// 4. GKE cluster with Workload Identity
// 5. Google Service Account: e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com
// 6. IAM for the GSA to read from the Artifact Registry repo
// 7. IAM for the test runner to write to Artifact Registry repo
// 8. gcloud & helm
func TestStressMemoryUsageHelm(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncKey := rootSyncID.ObjectKey
	nt := nomostest.New(t, nomostesting.WorkloadIdentity, ntopts.StressTest,
		ntopts.RequireHelmProvider,
		ntopts.SyncWithGitSource(nomostest.DefaultRootSyncID, ntopts.Unstructured),
		ntopts.WithReconcileTimeout(30*time.Second))

	if err := workloadidentity.ValidateEnabled(nt); err != nil {
		nt.T.Fatal(err)
	}
	gsaEmail := registryproviders.ArtifactRegistryReaderEmail()
	if err := iam.ValidateServiceAccountExists(nt, gsaEmail); err != nil {
		nt.T.Fatal(err)
	}

	chart, err := nt.BuildAndPushHelmPackage(rootSyncKey,
		registryproviders.HelmSourceChart("anvil-set"))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	ns := "stress-test-ns"

	crdCount := 100
	crCount := 50
	kind := "Anvil"

	nt.T.Logf("Updating RootSync to sync from the Helm chart: %s:%s", chart.Name, chart.Version)
	rs := nt.RootSyncObjectHelm(rootSyncID.Name, chart.HelmChartID)
	rs.Spec.Helm.Namespace = ns
	rs.Spec.Helm.Values = &apiextensionsv1.JSON{
		Raw: []byte(fmt.Sprintf(`{"resources": %d, "replicas": %d}`, crdCount, crCount)),
	}
	rs.Spec.SafeOverride().APIServerTimeout = &metav1.Duration{Duration: 30 * time.Second}
	if err := nt.KubeClient.Apply(rs); err != nil {
		nt.T.Fatal(err)
	}

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs(nomostest.WithTimeout(5 * time.Minute)))

	nt.T.Logf("Verify the number of Anvil objects")
	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v1",
		}
		nt.Must(validateNumberOfObjectsEquals(nt, gvk, crCount,
			client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
			client.InNamespace(ns)))
	}

	emptyChart, err := nt.BuildAndPushHelmPackage(rootSyncKey,
		registryproviders.HelmSourceChart("empty"),
		registryproviders.HelmChartVersion("v1.1.0"))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Logf("Updating RootSync to sync from the empty Helm chart: %s:%s", emptyChart.Name, emptyChart.Version)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"helm": {"chart": %q, "version": %q, "namespace": null, "values": null}}}`,
		emptyChart.Name, emptyChart.Version))

	// Update the expected helm chart
	nomostest.SetExpectedSyncSource(nt, rootSyncID, &syncsource.HelmSyncSource{
		ChartID: emptyChart.HelmChartID,
	})
	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	nt.Must(nt.WatchForAllSyncs(nomostest.WithTimeout(5 * time.Minute)))
}

func TestStressResourceGroup(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController, ntopts.StressTest)

	namespace := "resourcegroup-e2e"
	nt.T.Logf("Create test Namespace: %s", namespace)
	nt.T.Cleanup(func() {
		ns := k8sobjects.NamespaceObject(namespace)
		nt.Must(nt.KubeClient.DeleteIfExists(ns))
	})
	nt.Must(nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)))

	rgNN := types.NamespacedName{
		Name:      "group-a",
		Namespace: namespace,
	}
	nt.T.Logf("Create test ResourceGroup: %s", rgNN)
	inventoryID := rgNN.Name
	nt.T.Cleanup(func() {
		rg := testresourcegroup.New(rgNN, inventoryID)
		nt.Must(nt.KubeClient.DeleteIfExists(rg))
	})
	rg := testresourcegroup.New(rgNN, inventoryID)
	nt.Must(nt.KubeClient.Create(rg))

	numConfigMaps := 1000
	var resources []v1alpha1.ObjMetadata
	for i := 0; i < numConfigMaps; i++ {
		resources = append(resources, v1alpha1.ObjMetadata{
			Name:      fmt.Sprintf("example-%d", i),
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		})
	}

	nt.T.Logf("Update the ResourceGroup spec to add %d ConfigMaps", numConfigMaps)
	rg.Spec.Resources = resources
	nt.Must(nt.KubeClient.Update(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the applier")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Watch for ResourceGroup controller to update the ResourceGroup status")
	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 2
	expectedStatus.ResourceStatuses = testresourcegroup.GenerateResourceStatus(resources, v1alpha1.NotFound, "")
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	nt.T.Logf("Create %d ConfigMaps", numConfigMaps)
	nt.Must(testresourcegroup.CreateOrUpdateResources(nt.KubeClient, resources, inventoryID))

	nt.T.Log("Watch for ResourceGroup controller to update the ResourceGroup status")
	expectedStatus.ObservedGeneration = 2
	expectedStatus.ResourceStatuses = testresourcegroup.GenerateResourceStatus(resources, v1alpha1.Current, "1234567")
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	index := 995
	arbitraryConfigMapToDelete := v1.ConfigMap{}
	arbitraryConfigMapToDelete.Name = resources[index].Name
	arbitraryConfigMapToDelete.Namespace = resources[index].Namespace
	nt.T.Logf("Delete ConfigMap: %s", arbitraryConfigMapToDelete.Name)
	nt.Must(nt.KubeClient.Delete(&arbitraryConfigMapToDelete))

	nt.T.Log("Watch for ResourceGroup controller to update the ResourceGroup status")
	expectedStatus.ResourceStatuses[index] = v1alpha1.ResourceStatus{
		Status:      v1alpha1.NotFound,
		ObjMetadata: resources[index],
		Conditions: []v1alpha1.Condition{
			{
				Message: testresourcegroup.NotOwnedMessage,
				Reason:  v1alpha1.OwnershipEmpty,
				Status:  v1alpha1.UnknownConditionStatus,
				Type:    v1alpha1.Ownership,
			},
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	nt.T.Log("Assert that the controller pods did not restart")
	nt.Must(testresourcegroup.ValidateNoControllerPodRestarts(nt.KubeClient))
}

func fakeCRD(kind, group string) *apiextensionsv1.CustomResourceDefinition {
	singular := strings.ToLower(kind)
	plural := fmt.Sprintf("%ss", singular)
	crd := k8sobjects.CustomResourceDefinitionV1Object(core.Name(fmt.Sprintf("%s.%s", plural, group)))
	crd.Spec.Group = group
	crd.Spec.Names = apiextensionsv1.CustomResourceDefinitionNames{
		Plural:   plural,
		Singular: singular,
		Kind:     kind,
	}
	crd.Spec.Scope = apiextensionsv1.NamespaceScoped
	crd.Spec.Versions = []apiextensionsv1.CustomResourceDefinitionVersion{
		{
			Name:    "v1",
			Served:  true,
			Storage: false,
			Schema: &apiextensionsv1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {
							Type:     "object",
							Required: []string{"lbs"},
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"lbs": {
									Type:    "integer",
									Minimum: ptr.To(1.0),
									Maximum: ptr.To(9000.0),
								},
							},
						},
					},
				},
			},
		},
		{
			Name:    "v2",
			Served:  true,
			Storage: true,
			Schema: &apiextensionsv1.CustomResourceValidation{
				OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object",
					Properties: map[string]apiextensionsv1.JSONSchemaProps{
						"spec": {
							Type:     "object",
							Required: []string{"lbs"},
							Properties: map[string]apiextensionsv1.JSONSchemaProps{
								"lbs": {
									Type:    "integer",
									Minimum: ptr.To(1.0),
									Maximum: ptr.To(9000.0),
								},
							},
						},
					},
				},
			},
		},
	}
	return crd
}

func fakeCR(name, namespace string, gvk schema.GroupVersionKind) client.Object {
	cr := &unstructured.Unstructured{}
	cr.Object = map[string]interface{}{
		"spec": map[string]interface{}{
			"lbs": float64(1.0),
		},
	}
	cr.SetGroupVersionKind(gvk)
	cr.SetName(name)
	cr.SetNamespace(namespace)
	return cr
}

func truncateSourceErrors() testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return testpredicates.WrongTypeErr(o, &v1beta1.RepoSync{})
		}
		for _, cond := range rs.Status.Conditions {
			if cond.Type == v1beta1.RootSyncSyncing && cond.Status == metav1.ConditionFalse && cond.Reason == "Source" &&
				reflect.DeepEqual(cond.ErrorSourceRefs, []v1beta1.ErrorSource{v1beta1.SourceError}) && cond.ErrorSummary.Truncated {
				return nil
			}
		}
		return fmt.Errorf("the source errors should be truncated:\n%s", log.AsYAML(rs))
	}
}

func validateNumberOfObjectsEquals(nt *nomostest.NT, gvk schema.GroupVersionKind, count int, opts ...client.ListOption) error {
	nsList := &metav1.PartialObjectMetadataList{}
	nsList.SetGroupVersionKind(gvk)
	if err := nt.KubeClient.List(nsList, opts...); err != nil {
		return fmt.Errorf("listing objects: %w", err)
	}
	if len(nsList.Items) != count {
		return fmt.Errorf("expected cluster to have %d %s objects, but found %d", count, gvk, len(nsList.Items))
	}
	return nil
}

func pauseDeploymentObject(nt *nomostest.NT, name, namespace string) *appsv1.Deployment {
	labels := map[string]string{"app": name}
	deployment := k8sobjects.DeploymentObject(
		core.Name(name), core.Namespace(namespace), core.Labels(labels))
	deployment.Spec = appsv1.DeploymentSpec{
		Replicas: ptr.To(int32(1)),
		Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
		Selector: &metav1.LabelSelector{MatchLabels: labels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "pause",
						Image: "registry.k8s.io/pause:3.7",
						Ports: []v1.ContainerPort{
							{ContainerPort: 80},
						},
						// Default to minimum request for current autopilot clusters with bursting enabled
						// https://cloud.google.com/kubernetes-engine/docs/concepts/autopilot-resource-requests#compute-class-min-max
						Resources: v1.ResourceRequirements{
							Requests: v1.ResourceList{
								v1.ResourceCPU:              resource.MustParse("50m"),
								v1.ResourceMemory:           resource.MustParse("52Mi"),
								v1.ResourceEphemeralStorage: resource.MustParse("10Mi"), // otherwise defaults to 1Gi on autopilot
							},
							Limits: v1.ResourceList{
								v1.ResourceCPU:              resource.MustParse("50m"),
								v1.ResourceMemory:           resource.MustParse("52Mi"),
								v1.ResourceEphemeralStorage: resource.MustParse("10Mi"),
							},
						},
					},
				},
			},
		},
	}
	// For autopilot clusters which do not support bursting, minimum CPU is 250m and memory is 512Mi
	if nt.IsGKEAutopilot && !nt.ClusterSupportsBursting {
		deployment.Spec.Template.Spec.Containers[0].Resources = v1.ResourceRequirements{
			Requests: v1.ResourceList{
				v1.ResourceCPU:              resource.MustParse("250m"),
				v1.ResourceMemory:           resource.MustParse("512Mi"),
				v1.ResourceEphemeralStorage: resource.MustParse("10Mi"), // otherwise defaults to 1Gi
			},
			Limits: v1.ResourceList{
				v1.ResourceCPU:              resource.MustParse("250m"),
				v1.ResourceMemory:           resource.MustParse("512Mi"),
				v1.ResourceEphemeralStorage: resource.MustParse("10Mi"),
			},
		}
	}
	return deployment
}
