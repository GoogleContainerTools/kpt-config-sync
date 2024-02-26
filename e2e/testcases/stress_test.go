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

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/iam"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/registryproviders"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testresourcegroup"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/testing/fake"
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
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	syncPath := gitproviders.DefaultSyncDir

	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	crdName := "crontabs.stable.example.com"
	nt.T.Logf("Delete the %q CRD if needed", crdName)
	nt.MustKubectl("delete", "crd", crdName, "--ignore-not-found")

	crdContent, err := os.ReadFile("../testdata/customresources/changed_schema_crds/old_schema_crd.yaml")
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].AddFile(fmt.Sprintf("%s/crontab-crd.yaml", syncPath), crdContent))

	labelKey := "StressTestName"
	labelValue := "TestStressCRD"
	for i := 1; i <= 1000; i++ {
		ns := fmt.Sprintf("foo%d", i)
		nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/ns-%d.yaml", syncPath, i), fake.NamespaceObject(
			ns, core.Label(labelKey, labelValue))))
		nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/cm-%d.yaml", syncPath, i), fake.ConfigMapObject(
			core.Name("cm1"), core.Namespace(ns), core.Label(labelKey, labelValue))))

		cr, err := crontabCR(ns, "cr1")
		if err != nil {
			nt.T.Fatal(err)
		}
		nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/crontab-cr-%d.yaml", syncPath, i), cr))
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add configs (one CRD and 1000 Namespaces (every namespace has one ConfigMap and one CR)"))
	err = nt.WatchForAllSyncs(nomostest.WithTimeout(30 * time.Minute))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify that the CronTab CRD is installed on the cluster")
	if err := nt.Validate(crdName, "", fake.CustomResourceDefinitionV1Object()); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify that there are exactly 1000 Namespaces managed by Config Sync on the cluster")
	validateNumberOfObjectsEquals(nt, kinds.Namespace(), 1000,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue})

	nt.T.Logf("Verify that there are exactly 1000 ConfigMaps managed by Config Sync on the cluster")
	validateNumberOfObjectsEquals(nt, kinds.ConfigMap(), 1000,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue})

	nt.T.Logf("Verify that there are exactly 1000 CronTab CRs managed by Config Sync on the cluster")
	validateNumberOfObjectsEquals(nt, crontabGVK, 1000,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue})
}

// TestStressLargeNamespace tests that Config Sync can sync a namespace including 5000 resources successfully.
func TestStressLargeNamespace(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	syncPath := gitproviders.DefaultSyncDir
	ns := "my-ns-1"

	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)

	nt.T.Log("Override the memory limit of the reconciler container of root-reconciler to 800MiB")
	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "reconciler", "memoryLimit": "800Mi"}]}}}`)
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), fake.NamespaceObject(ns)))

	labelKey := "StressTestName"
	labelValue := "TestStressLargeNamespace"
	for i := 1; i <= 5000; i++ {
		nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/cm-%d.yaml", syncPath, i), fake.ConfigMapObject(
			core.Name(fmt.Sprintf("cm-%d", i)), core.Namespace(ns), core.Label(labelKey, labelValue))))
	}
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add configs (5000 ConfigMaps and 1 Namespace"))
	err := nt.WatchForAllSyncs(nomostest.WithTimeout(10 * time.Minute))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Verify there are 5000 ConfigMaps in the namespace")
	validateNumberOfObjectsEquals(nt, kinds.ConfigMap(), 5000,
		client.MatchingLabels{labelKey: labelValue},
		client.InNamespace(ns))
}

// TestStressFrequentGitCommits adds 100 Git commits, and verifies that Config Sync can sync the changes in these commits successfully.
func TestStressFrequentGitCommits(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	syncPath := gitproviders.DefaultSyncDir
	ns := "bookstore"

	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)

	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), fake.NamespaceObject(ns)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("add a namespace: %s", ns)))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Add 100 commits (every commit adds a new ConfigMap object into the %s namespace)", ns)
	labelKey := "StressTestName"
	labelValue := "TestStressFrequentGitCommits"
	for i := 0; i < 100; i++ {
		cmName := fmt.Sprintf("cm-%v", i)
		nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/%s.yaml", syncPath, cmName),
			fake.ConfigMapObject(core.Name(cmName), core.Namespace(ns), core.Label(labelKey, labelValue))))
		nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("add %s", cmName)))
	}
	err := nt.WatchForAllSyncs(nomostest.WithTimeout(10 * time.Minute))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify that there are exactly 100 ConfigMaps under the %s namespace", ns)
	validateNumberOfObjectsEquals(nt, kinds.ConfigMap(), 100,
		client.MatchingLabels{labelKey: labelValue},
		client.InNamespace(ns))
}

// This test creates a RootSync pointed at https://github.com/config-sync-examples/crontab-crs
// This repository contains 13,000+ objects, which takes a long time to reconcile
func TestStressLargeRequest(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	crdName := "crontabs.stable.example.com"

	oldCrdFilePath := "../testdata/customresources/changed_schema_crds/old_schema_crd.yaml"
	nt.T.Logf("Apply the old CronTab CRD defined in %s", oldCrdFilePath)
	nt.MustKubectl("apply", "-f", oldCrdFilePath)

	nt.T.Logf("Wait until the old CRD is established")
	err := nt.Watcher.WatchObject(kinds.CustomResourceDefinitionV1(), crdName, "",
		[]testpredicates.Predicate{nomostest.IsEstablished})
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSyncFilePath := "../testdata/root-sync-crontab-crs.yaml"
	nt.T.Logf("Apply the RootSync object defined in %s", rootSyncFilePath)
	nt.MustKubectl("apply", "-f", rootSyncFilePath)

	nt.T.Logf("Verify that the source errors are truncated")
	err = nt.Watcher.WatchObject(kinds.RootSyncV1Beta1(), "root-sync", configmanagement.ControllerNamespace,
		[]testpredicates.Predicate{truncateSourceErrors()})
	if err != nil {
		nt.T.Fatal(err)
	}

	newCrdFilePath := "../testdata/customresources/changed_schema_crds/new_schema_crd.yaml"
	nt.T.Logf("Apply the new CronTab CRD defined in %s", newCrdFilePath)
	nt.MustKubectl("apply", "-f", newCrdFilePath)

	nt.T.Logf("Wait until the new CRD is established")
	err = nt.Watcher.WatchObject(kinds.CustomResourceDefinitionV1(), crdName, "",
		[]testpredicates.Predicate{nomostest.IsEstablished})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Wait for the sync to complete")
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: "configs",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestStress100CRDs applies 100 CRDs and validates that syncing still works.
// This simulates a scenario similar to using Config Connector or Crossplane.
// This test also validates that nomos status does not use client-side throttling.
func TestStress100CRDs(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

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
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), fake.NamespaceObject(ns)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add a test namespace to trigger a new sync"))

	nt.T.Logf("Waiting for the sync to complete")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

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
func TestStressManyDeployments(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured,
		ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	syncPath := filepath.Join(gitproviders.DefaultSyncDir, "stress-test")
	ns := "stress-test-ns"

	deployCount := 1000

	nt.T.Logf("Adding a test namespace and %d deployments", deployCount)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), fake.NamespaceObject(ns)))

	for i := 1; i <= deployCount; i++ {
		name := fmt.Sprintf("pause-%d", i)
		nt.Must(nt.RootRepos[configsync.RootSyncName].AddFile(
			fmt.Sprintf("%s/namespaces/%s/deployment-%s.yaml", syncPath, ns, name),
			[]byte(fmt.Sprintf(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %s
  namespace: %s
  labels:
    app: %s
    nomos-test: enabled
spec:
  replicas: 1
  selector:
    matchLabels:
      app: %s
  template:
    metadata:
      labels:
        app: %s
    spec:
      containers:
      - name: pause
        image: registry.k8s.io/pause:3.7
        ports:
        - containerPort: 80
        resources:
          limits:
            cpu: 250m
            memory: 256Mi
          requests:
            cpu: 250m
            memory: 256Mi
`, name, ns, name, name, name))))
	}

	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Adding a test namespace and %d deployments", deployCount)))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify the number of Deployment objects")
	validateNumberOfObjectsEquals(nt, kinds.Deployment(), deployCount,
		client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
		client.InNamespace(ns))

	nt.T.Log("Removing resources from Git")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(syncPath))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing resources from Git"))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

// TestStressMemoryUsageGit applies 100 CRDs and then 50 objects for each
// resource. This stressed both the number of watches and the number of objects,
// which increases memory usage.
func TestStressMemoryUsageGit(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.Unstructured, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	syncPath := filepath.Join(gitproviders.DefaultSyncDir, "stress-test")
	ns := "stress-test-ns"
	kind := "Anvil"

	crdCount := 100
	crCount := 50

	nt.T.Logf("Adding a test namespace, %d crds, and %d objects per crd", crdCount, crCount)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/ns-%s.yaml", syncPath, ns), fake.NamespaceObject(ns)))

	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		crd := fakeCRD(kind, group)
		nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/crd-%s.yaml", syncPath, crd.Name), crd))
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v2",
		}
		for j := 1; j <= crCount; j++ {
			cr := fakeCR(fmt.Sprintf("%s-%d", strings.ToLower(kind), j), ns, gvk)
			nt.Must(nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("%s/namespaces/%s/%s-%s.yaml", syncPath, ns, crd.Name, cr.GetName()), cr))
		}
	}

	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("Adding a test namespace, %d crds, and %d objects per crd", crdCount, crCount)))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify the number of Anvil objects")
	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v2",
		}
		validateNumberOfObjectsEquals(nt, gvk, crCount,
			client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
			client.InNamespace(ns))
	}

	nt.T.Log("Removing resources from Git")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove(syncPath))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing resources from Git"))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
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
	nt := nomostest.New(t, nomostesting.WorkloadIdentity, ntopts.Unstructured,
		ntopts.StressTest, ntopts.RequireOCIProvider,
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
	packageObjs = append(packageObjs, fake.NamespaceObject(ns))

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

	image, err := nt.BuildAndPushOCIImage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.ImageInputObjects(nt.Scheme, packageObjs...))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Update RootSync to sync from the OCI image in Artifact Registry")
	rs := nt.RootSyncObjectOCI(configsync.RootSyncName, image)
	if err := nt.KubeClient.Apply(rs); err != nil {
		nt.T.Fatal(err)
	}

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByDigest(image.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: ".",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify the number of Anvil objects")
	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v2",
		}
		validateNumberOfObjectsEquals(nt, gvk, crCount,
			client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
			client.InNamespace(ns))
	}

	nt.T.Log("Remove all files and publish an empty OCI image")
	emptyImage, err := nt.BuildAndPushOCIImage(nomostest.RootSyncNN(configsync.RootSyncName))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	err = nt.WatchForAllSyncs(
		nomostest.WithRootSha1Func(imageDigestFuncByDigest(emptyImage.Digest)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: ".",
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
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
	nt := nomostest.New(t, nomostesting.WorkloadIdentity, ntopts.Unstructured,
		ntopts.StressTest, ntopts.RequireHelmProvider,
		ntopts.WithReconcileTimeout(30*time.Second))

	if err := workloadidentity.ValidateEnabled(nt); err != nil {
		nt.T.Fatal(err)
	}
	gsaEmail := registryproviders.ArtifactRegistryReaderEmail()
	if err := iam.ValidateServiceAccountExists(nt, gsaEmail); err != nil {
		nt.T.Fatal(err)
	}

	chart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart("anvil-set"))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	ns := "stress-test-ns"

	crdCount := 100
	crCount := 50
	kind := "Anvil"

	nt.T.Logf("Updating RootSync to sync from the Helm chart: %s:%s", chart.Name, chart.Version)
	rs := nt.RootSyncObjectHelm(configsync.RootSyncName, chart)
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
	err = nt.WatchForAllSyncs(
		nomostest.WithTimeout(5*time.Minute),
		nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(chart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: chart.Name,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify the number of Anvil objects")
	for i := 1; i <= crdCount; i++ {
		group := fmt.Sprintf("acme-%d.com", i)
		gvk := schema.GroupVersionKind{
			Group:   group,
			Kind:    kind,
			Version: "v1",
		}
		validateNumberOfObjectsEquals(nt, gvk, crCount,
			client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue},
			client.InNamespace(ns))
	}

	emptyChart, err := nt.BuildAndPushHelmPackage(nomostest.RootSyncNN(configsync.RootSyncName),
		registryproviders.HelmSourceChart("empty"),
		registryproviders.HelmChartVersion("v1.1.0"))
	if err != nil {
		nt.T.Fatalf("failed to push helm chart: %v", err)
	}

	nt.T.Logf("Updating RootSync to sync from the empty Helm chart: %s:%s", emptyChart.Name, emptyChart.Version)
	nt.MustMergePatch(rs, fmt.Sprintf(`{"spec": {"helm": {"chart": %q, "version": %q, "namespace": null, "values": null}}}`,
		emptyChart.Name, emptyChart.Version))

	// Validate that the resources sync without the reconciler running out of
	// memory, getting OOMKilled, and crash looping.
	err = nt.WatchForAllSyncs(
		nomostest.WithTimeout(5*time.Minute),
		nomostest.WithRootSha1Func(nomostest.HelmChartVersionShaFn(emptyChart.Version)),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{
			nomostest.DefaultRootRepoNamespacedName: emptyChart.Name,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestStressResourceGroup(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController, ntopts.StressTest)

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		// all test resources are created in this namespace
		if err := nt.KubeClient.Delete(fake.NamespaceObject(namespace)); err != nil {
			nt.T.Error(err)
		}
	})
	if err := nt.KubeClient.Create(fake.NamespaceObject(namespace)); err != nil {
		nt.T.Fatal(err)
	}
	rgNN := types.NamespacedName{
		Name:      "group-a",
		Namespace: namespace,
	}
	resourceID := rgNN.Name
	rg := testresourcegroup.New(rgNN, resourceID)
	if err := nt.KubeClient.Create(rg); err != nil {
		nt.T.Fatal(err)
	}

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

	err := testresourcegroup.UpdateResourceGroup(nt.KubeClient, rgNN, func(rg *v1alpha1.ResourceGroup) {
		rg.Spec.Resources = resources
	})
	if err != nil {
		nt.T.Fatal(err)
	}
	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 2
	expectedStatus.ResourceStatuses = testresourcegroup.GenerateResourceStatus(resources, v1alpha1.NotFound, "")

	err = nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace, []testpredicates.Predicate{
		testpredicates.ResourceGroupStatusEquals(expectedStatus),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	if err := testresourcegroup.CreateOrUpdateResources(nt.KubeClient, resources, resourceID); err != nil {
		nt.T.Fatal(err)
	}

	expectedStatus.ObservedGeneration = 2
	expectedStatus.ResourceStatuses = testresourcegroup.GenerateResourceStatus(resources, v1alpha1.Current, "1234567")

	err = nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace, []testpredicates.Predicate{
		testpredicates.ResourceGroupStatusEquals(expectedStatus),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	index := 995
	arbitraryConfigMapToDelete := v1.ConfigMap{}
	arbitraryConfigMapToDelete.Name = resources[index].Name
	arbitraryConfigMapToDelete.Namespace = resources[index].Namespace
	if err := nt.KubeClient.Delete(&arbitraryConfigMapToDelete); err != nil {
		nt.T.Fatal(err)
	}
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

	err = nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace, []testpredicates.Predicate{
		testpredicates.ResourceGroupStatusEquals(expectedStatus),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Delete the ResourceGroup")
	if err := nt.KubeClient.Delete(rg); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Log("Assert that the controller pods did not restart")
	if err := testresourcegroup.ValidateNoControllerPodRestarts(nt.KubeClient); err != nil {
		nt.T.Fatal(err)
	}
}

func fakeCRD(kind, group string) *apiextensionsv1.CustomResourceDefinition {
	singular := strings.ToLower(kind)
	plural := fmt.Sprintf("%ss", singular)
	crd := fake.CustomResourceDefinitionV1Object(core.Name(fmt.Sprintf("%s.%s", plural, group)))
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
									Minimum: pointer.Float64(1.0),
									Maximum: pointer.Float64(9000.0),
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
									Minimum: pointer.Float64(1.0),
									Maximum: pointer.Float64(9000.0),
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
		return errors.Errorf("the source errors should be truncated")
	}
}

func validateNumberOfObjectsEquals(nt *nomostest.NT, gvk schema.GroupVersionKind, count int, opts ...client.ListOption) {
	nsList := &metav1.PartialObjectMetadataList{}
	nsList.SetGroupVersionKind(gvk)
	if err := nt.KubeClient.List(nsList, opts...); err != nil {
		nt.T.Error(err)
	}
	if len(nsList.Items) != count {
		nt.T.Errorf("Expected cluster to have %d %s objects, but found %d", count, gvk, len(nsList.Items))
	}
}
