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
	"reflect"
	"testing"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
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
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.SkipMonoRepo, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)
	nt.WaitForRepoSyncs()

	crdName := "crontabs.stable.example.com"
	nt.T.Logf("Delete the %q CRD if needed", crdName)
	nt.MustKubectl("delete", "crd", crdName, "--ignore-not-found")

	crdContent, err := ioutil.ReadFile("../testdata/customresources/changed_schema_crds/old_schema_crd.yaml")
	if err != nil {
		nt.T.Fatal(err)
	}
	nt.RootRepos[configsync.RootSyncName].AddFile("acme/crontab-crd.yaml", crdContent)

	labelKey := "StressTestName"
	labelValue := "TestStressCRD"
	for i := 1; i <= 1000; i++ {
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/ns-%d.yaml", i), fake.NamespaceObject(
			fmt.Sprintf("foo%d", i), core.Label(labelKey, labelValue)))
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/cm-%d.yaml", i), fake.ConfigMapObject(
			core.Name("cm1"), core.Namespace(fmt.Sprintf("foo%d", i)), core.Label(labelKey, labelValue)))

		cr, err := crontabCR(fmt.Sprintf("foo%d", i), "cr1")
		if err != nil {
			nt.T.Fatal(err)
		}
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/crontab-cr-%d.yaml", i), cr)
	}
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add configs (one CRD and 1000 Namespaces (every namespace has one ConfigMap and one CR)")
	nt.WaitForRepoSyncs(nomostest.WithTimeout(30 * time.Minute))

	nt.T.Logf("Verify that the CronTab CRD is installed on the cluster")
	if err := nt.Validate(crdName, "", fake.CustomResourceDefinitionV1Object()); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Verify that there are exactly 1000 Namespaces managed by Config Sync on the cluster")
	nsList := &corev1.NamespaceList{}
	if err := nt.Client.List(nt.Context, nsList, client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(nsList.Items) != 1000 {
		nt.T.Errorf("The cluster should include 1000 Namespaces managed by Config Sync and having the `%s: %s` label exactly, found %v instead", labelKey, labelValue, len(nsList.Items))
	}

	nt.T.Logf("Verify that there are exactly 1000 ConfigMaps managed by Config Sync on the cluster")
	cmList := &corev1.ConfigMapList{}
	if err := nt.Client.List(nt.Context, cmList, client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue, labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(cmList.Items) != 1000 {
		nt.T.Errorf("The cluster should include 1000 ConfigMaps managed by Config Sync and having the `%s: %s` label exactly, found %v instead", labelKey, labelValue, len(cmList.Items))
	}

	nt.T.Logf("Verify that there are exactly 1000 CronTab CRs managed by Config Sync on the cluster")
	crList := &unstructured.UnstructuredList{}
	crList.SetGroupVersionKind(crontabGVK)
	if err := nt.Client.List(nt.Context, crList, client.MatchingLabels{metadata.ManagedByKey: metadata.ManagedByValue}); err != nil {
		nt.T.Error(err)
	}
	if len(crList.Items) != 1000 {
		nt.T.Errorf("The cluster should include 1000 CronTab managed by Config Sync exactly, found %v instead", len(crList.Items))
	}
}

// TestStressLargeNamespace tests that Config Sync can sync a namespace including 5000 resources successfully.
func TestStressLargeNamespace(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.SkipMonoRepo, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	nt.T.Log("Stop the CS webhook by removing the webhook configuration")
	nomostest.StopWebhook(nt)

	nt.T.Log("Override the memory limit of the reconciler container of root-reconciler to 800MiB")
	rootSync := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rootSync, `{"spec": {"override": {"resources": [{"containerName": "reconciler", "memoryLimit": "800Mi"}]}}}`)
	nt.WaitForRepoSyncs()

	ns := "my-ns-1"
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", fake.NamespaceObject(ns))

	labelKey := "StressTestName"
	labelValue := "TestStressLargeNamespace"
	for i := 1; i <= 5000; i++ {
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/cm-%d.yaml", i), fake.ConfigMapObject(
			core.Name(fmt.Sprintf("cm-%d", i)), core.Namespace(ns), core.Label(labelKey, labelValue)))
	}
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Add configs (5000 ConfigMaps and 1 Namespace")
	nt.WaitForRepoSyncs(nomostest.WithTimeout(10 * time.Minute))

	nt.T.Log("Verify there are 5000 ConfigMaps in the namespace")
	cmList := &corev1.ConfigMapList{}

	if err := nt.Client.List(nt.Context, cmList, &client.ListOptions{Namespace: ns}, client.MatchingLabels{labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(cmList.Items) != 5000 {
		nt.T.Errorf("The %s namespace should include 5000 ConfigMaps having the `%s: %s` label exactly, found %v instead", ns, labelKey, labelValue, len(cmList.Items))
	}
}

// TestStressFrequentGitCommits adds 100 Git commits, and verifies that Config Sync can sync the changes in these commits successfully.
func TestStressFrequentGitCommits(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.StressTest,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))
	if nt.MultiRepo {
		nt.T.Log("Stop the CS webhook by removing the webhook configuration")
		nomostest.StopWebhook(nt)
	}

	ns := "bookstore"
	namespace := fake.NamespaceObject(ns)
	nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("add a namespace: %s", ns))
	nt.WaitForRepoSyncs()

	nt.T.Logf("Add 100 commits (every commit adds a new ConfigMap object into the %s namespace)", ns)
	labelKey := "StressTestName"
	labelValue := "TestStressFrequentGitCommits"
	for i := 0; i < 100; i++ {
		cmName := fmt.Sprintf("cm-%v", i)
		nt.RootRepos[configsync.RootSyncName].Add(fmt.Sprintf("acme/%s.yaml", cmName), fake.ConfigMapObject(core.Name(cmName), core.Namespace(ns), core.Label(labelKey, labelValue)))
		nt.RootRepos[configsync.RootSyncName].CommitAndPush(fmt.Sprintf("add %s", cmName))
	}
	nt.WaitForRepoSyncs(nomostest.WithTimeout(10 * time.Minute))

	nt.T.Logf("Verify that there are exactly 100 ConfigMaps under the %s namespace", ns)
	cmList := &corev1.ConfigMapList{}
	if err := nt.Client.List(nt.Context, cmList, &client.ListOptions{Namespace: ns}, client.MatchingLabels{labelKey: labelValue}); err != nil {
		nt.T.Error(err)
	}
	if len(cmList.Items) != 100 {
		nt.T.Errorf("The %s namespace should include 100 ConfigMaps having the `%s: %s` label exactly, found %v instead", ns, labelKey, labelValue, len(cmList.Items))
	}
}

func TestStressLargeRequest(t *testing.T) {
	nt := nomostest.New(t, ntopts.Unstructured, ntopts.StressTest, ntopts.SkipMonoRepo,
		ntopts.WithReconcileTimeout(configsync.DefaultReconcileTimeout))

	crdName := "crontabs.stable.example.com"

	oldCrdFilePath := "../testdata/customresources/changed_schema_crds/old_schema_crd.yaml"
	nt.T.Logf("Apply the old CronTab CRD defined in %s", oldCrdFilePath)
	nt.MustKubectl("apply", "-f", oldCrdFilePath)

	nt.T.Logf("Wait until the old CRD is established")
	_, err := nomostest.Retry(nt.DefaultWaitTimeout, func() error {
		return nt.Validate(crdName, "", fake.CustomResourceDefinitionV1Object(), nomostest.IsEstablished)
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSyncFilePath := "../testdata/root-sync-crontab-crs.yaml"
	nt.T.Logf("Apply the RootSync object defined in %s", rootSyncFilePath)
	nt.MustKubectl("apply", "-f", rootSyncFilePath)

	nt.T.Logf("Verify that the source errors are truncated")
	_, err = nomostest.Retry(5*time.Minute, func() error {
		return nt.Validate("root-sync", configmanagement.ControllerNamespace, fake.RootSyncObjectV1Beta1(configsync.RootSyncName), truncateSourceErrors())
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	newCrdFilePath := "../testdata/customresources/changed_schema_crds/new_schema_crd.yaml"
	nt.T.Logf("Apply the new CronTab CRD defined in %s", newCrdFilePath)
	nt.MustKubectl("apply", "-f", newCrdFilePath)

	nt.T.Logf("Wait until the new CRD is established")
	_, err = nomostest.Retry(nt.DefaultWaitTimeout, func() error {
		return nt.Validate(crdName, "", fake.CustomResourceDefinitionV1Object(), nomostest.IsEstablished)
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Logf("Wait for the sync to complete")
	nt.WaitForRepoSyncs(nomostest.WithRootSha1Func(nomostest.RemoteRootRepoSha1Fn),
		nomostest.WithSyncDirectoryMap(map[types.NamespacedName]string{nomostest.DefaultRootRepoNamespacedName: "configs"}),
		nomostest.WithTimeout(30*time.Minute))
}

func truncateSourceErrors() nomostest.Predicate {
	return func(o client.Object) error {
		rs, ok := o.(*v1beta1.RootSync)
		if !ok {
			return nomostest.WrongTypeErr(o, &v1beta1.RepoSync{})
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
