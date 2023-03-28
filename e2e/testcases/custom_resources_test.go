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

	admissionv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
	"kpt.dev/configsync/pkg/webhook/configuration"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCRDDeleteBeforeRemoveCustomResourceV1Beta1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1)

	support, err := nt.SupportV1Beta1CRDAndRBAC()
	if err != nil {
		nt.T.Fatal("failed to check the supported CRD versions")
	}
	// Skip this test if v1beta1 CRD is not supported in the testing cluster.
	if !support {
		return
	}

	crdFile := filepath.Join(".", "..", "testdata", "customresources", "v1beta1_crds", "anvil-crd.yaml")
	clusterFile := filepath.Join(".", "..", "testdata", "customresources", "v1beta1_crds", "clusteranvil-crd.yaml")
	_, err = nt.Shell.Kubectl("apply", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	_, err = nt.Shell.Kubectl("apply", "-f", clusterFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.WaitForCRDs(nt, []string{"anvils.acme.com", "clusteranvils.acme.com"})
	if err != nil {
		nt.T.Fatal(err)
	}

	nsObj := fake.NamespaceObject("prod")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/ns.yaml", nsObj)
	anvilObj := anvilCR("v1", "heavy", 10)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/anvil-v1.yaml", anvilObj)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Reset discovery client to pick up Anvil CRD
	nt.RenewClient()

	err = nt.Validate(configuration.Name, "", &admissionv1.ValidatingWebhookConfiguration{},
		hasRule("acme.com.v1.admission-webhook.configsync.gke.io"))
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Watcher.WatchForCurrentStatus(anvilGVK("v1"), "heavy", "prod",
		testwatcher.WatchTimeout(60*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, anvilObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove CRD
	// This will garbage collect the CR too and block until both are deleted.
	_, err = nt.Shell.Kubectl("delete", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Reset discovery client to invalidate the cached Anvil CRD
	nt.RenewClient()

	// Modify the Anvil yaml to trigger immediate re-sync, instead of waiting
	// for automatic retry (1hr default).
	anvilObj = anvilCR("v1", "heavy", 100)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/anvil-v1.yaml", anvilObj)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify Anvil CR")

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode, "")

	rootReconcilerPod, err := nt.KubeClient.GetDeploymentPod(
		nomostest.DefaultRootReconcilerName, configmanagement.ControllerNamespace,
		nt.DefaultWaitTimeout)
	if err != nil {
		nt.T.Fatal(err)
	}

	commitHash := nt.RootRepos[configsync.RootSyncName].Hash()

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootReconcilerPod.Name, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the CR.
	// This should fix the error.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/prod/anvil-v1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing the Anvil CR as well")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, anvilObj)

	// Validate reconciler error is cleared.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: nomostest.RootSyncNN(configsync.RootSyncName),
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestCRDDeleteBeforeRemoveCustomResourceV1(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1)

	crdFile := filepath.Join(".", "..", "testdata", "customresources", "v1_crds", "anvil-crd.yaml")
	clusterFile := filepath.Join(".", "..", "testdata", "customresources", "v1_crds", "clusteranvil-crd.yaml")
	_, err := nt.Shell.Kubectl("apply", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}
	_, err = nt.Shell.Kubectl("apply", "-f", clusterFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.WaitForCRDs(nt, []string{"anvils.acme.com", "clusteranvils.acme.com"})
	if err != nil {
		nt.T.Fatal(err)
	}

	nsObj := fake.NamespaceObject("foo")
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", nsObj)
	anvilObj := anvilCR("v1", "heavy", 10)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilObj)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR")
	firstCommitHash := nt.RootRepos[configsync.RootSyncName].Hash()

	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Reset discovery client to pick up Anvil CRD
	nt.RenewClient()

	err = nt.Validate(configuration.Name, "", &admissionv1.ValidatingWebhookConfiguration{},
		hasRule("acme.com.v1.admission-webhook.configsync.gke.io"))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Use Retry & Validate instead of WatchForCurrentStatus, because
	// watching would require creating API objects and updating the scheme.
	_, err = retry.Retry(60*time.Second, func() error {
		return nt.Validate("heavy", "foo", anvilCR("v1", "", 0),
			testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus))
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, nsObj)
	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, anvilObj)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove CRD
	// This will garbage collect the CR too and block until both are deleted.
	_, err = nt.Shell.Kubectl("delete", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Reset discovery client to invalidate the cached Anvil CRD
	nt.RenewClient()

	// Modify the Anvil yaml to trigger immediate re-sync, instead of waiting
	// for automatic retry (1hr default).
	anvilObj = anvilCR("v1", "heavy", 100)
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilObj)
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify Anvil CR")
	secondCommitHash := nt.RootRepos[configsync.RootSyncName].Hash()

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode, "")

	rootReconcilerPod, err := nt.KubeClient.GetDeploymentPod(
		nomostest.DefaultRootReconcilerName, configmanagement.ControllerNamespace,
		nt.DefaultWaitTimeout)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootReconcilerPod.Name, firstCommitHash, metrics.ErrorSummary{
			// Remediator conflict after the first commit, because the declared
			// Anvil was deleted by another client after successful sync.
			Conflicts: 1,
		}),
		nomostest.ReconcilerErrorMetrics(nt, rootReconcilerPod.Name, secondCommitHash, metrics.ErrorSummary{
			// No remediator conflict after the second commit, because the
			// reconciler hasn't been updated with the latest declared resources,
			// because there was a source error.
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the CR.
	// This should fix the error.
	nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/anvil-v1.yaml")
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing the Anvil CR as well")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// CR wasn't added to the inventory, so it won't be deleted.
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, anvilObj)

	// Validate reconciler error is cleared.
	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestSyncUpdateCustomResource(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1)
	support, err := nt.SupportV1Beta1CRDAndRBAC()
	if err != nil {
		nt.T.Fatal("failed to check the supported CRD versions")
	}
	// Skip this test if v1beta1 CRD is not supported in the testing cluster.
	if !support {
		return
	}

	crdFile := filepath.Join(".", "..", "testdata", "customresources", "v1beta1_crds", "anvil-crd-structural-v1.yaml")
	_, err = nt.Shell.Kubectl("apply", "-f", crdFile)
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nt.Watcher.WatchObject(kinds.CustomResourceDefinitionV1(), "anvils.acme.com", "",
		[]testpredicates.Predicate{nomostest.IsEstablished},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo"))
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 10))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.RenewClient()

	err = nt.Validate("heavy", "foo", anvilCR("v1", "", 0), weightEqual10)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update CustomResource
	nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 100))
	nt.RootRepos[configsync.RootSyncName].CommitAndPush("Updating Anvil CR")
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.RenewClient()

	err = nt.Validate("heavy", "foo", anvilCR("v1", "", 0), weightEqual100)
	if err != nil {
		nt.T.Fatal(err)
	}

}

func weightEqual100(obj client.Object) error {
	if obj == nil {
		return testpredicates.ErrObjectNotFound
	}
	u := obj.(*unstructured.Unstructured)
	val, _, err := unstructured.NestedInt64(u.Object, "spec", "lbs")
	if err != nil {
		return err
	}
	if val != 100 {
		return fmt.Errorf(".spec.lbs should be 100 but got %d", val)
	}
	return nil
}

func weightEqual10(obj client.Object) error {
	if obj == nil {
		return testpredicates.ErrObjectNotFound
	}
	u := obj.(*unstructured.Unstructured)
	val, _, err := unstructured.NestedInt64(u.Object, "spec", "lbs")
	if err != nil {
		return err
	}
	if val != 10 {
		return fmt.Errorf(".spec.lbs should be 10 but got %d", val)
	}
	return nil
}
