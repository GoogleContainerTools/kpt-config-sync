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
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
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

	crdContent, err := ioutil.ReadFile("../testdata/customresources/changed_schema_crds/old_schema_crd.yaml")
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
	nsList := &metav1.PartialObjectMetadataList{}
	nsList.SetGroupVersionKind(kinds.Namespace())
	if err := nt.KubeClient.List(nsList, client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(nsList.Items) != 1000 {
		nt.T.Errorf("The cluster should include 1000 Namespaces managed by Config Sync and having the `%s: %s` label exactly, found %v instead", labelKey, labelValue, len(nsList.Items))
	}

	nt.T.Logf("Verify that there are exactly 1000 ConfigMaps managed by Config Sync on the cluster")
	cmList := &metav1.PartialObjectMetadataList{}
	cmList.SetGroupVersionKind(kinds.ConfigMap())
	if err := nt.KubeClient.List(cmList, client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(cmList.Items) != 1000 {
		nt.T.Errorf("The cluster should include 1000 ConfigMaps managed by Config Sync and having the `%s: %s` label exactly, found %v instead", labelKey, labelValue, len(cmList.Items))
	}

	nt.T.Logf("Verify that there are exactly 1000 CronTab CRs managed by Config Sync on the cluster")
	crList := &metav1.PartialObjectMetadataList{}
	crList.SetGroupVersionKind(crontabGVK)
	if err := nt.KubeClient.List(crList, client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue}); err != nil {
		nt.T.Error(err)
	}
	if len(crList.Items) != 1000 {
		nt.T.Errorf("The cluster should include 1000 CronTab managed by Config Sync exactly, found %v instead", len(crList.Items))
	}
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
	cmList := &metav1.PartialObjectMetadataList{}
	cmList.SetGroupVersionKind(kinds.ConfigMap())
	if err := nt.KubeClient.List(cmList, &client.ListOptions{Namespace: ns}, client.MatchingLabels{labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(cmList.Items) != 5000 {
		nt.T.Errorf("The %s namespace should include 5000 ConfigMaps having the `%s: %s` label exactly, found %v instead", ns, labelKey, labelValue, len(cmList.Items))
	}
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
	cmList := &metav1.PartialObjectMetadataList{}
	cmList.SetGroupVersionKind(kinds.ConfigMap())
	if err := nt.KubeClient.List(cmList, &client.ListOptions{Namespace: ns}, client.MatchingLabels{labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(cmList.Items) != 100 {
		nt.T.Errorf("The %s namespace should include 100 ConfigMaps having the `%s: %s` label exactly, found %v instead", ns, labelKey, labelValue, len(cmList.Items))
	}
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

// TestStressMemoryUsage applies 100 CRDs and then 10 objects for each resource.
// This stressed both the number of watches and the number of objects,
// which increases memory usage.
// TODO: Make the objects larger to increase memory usage even more.
func TestStressMemoryUsage(t *testing.T) {
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
		nsList := &metav1.PartialObjectMetadataList{}
		nsList.SetGroupVersionKind(gvk)
		if err := nt.KubeClient.List(nsList, client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue}, client.InNamespace(ns)); err != nil {
			nt.T.Error(err)
		}
		if len(nsList.Items) != crCount {
			nt.T.Errorf("The cluster should include %d anvils.%s objects, found %v instead", crCount, group, len(nsList.Items))
		}
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
