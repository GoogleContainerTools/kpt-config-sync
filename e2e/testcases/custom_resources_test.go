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
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/ns.yaml", nsObj))
	anvilObj := anvilCR("v1", "heavy", 10)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/anvil-v1.yaml", anvilObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR"))
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
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/prod/anvil-v1.yaml", anvilObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify Anvil CR"))

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode, "")

	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}
	commitHash := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)

	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, commitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the CR.
	// This should fix the error.
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/prod/anvil-v1.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing the Anvil CR as well"))
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
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", nsObj))
	anvilObj := anvilCR("v1", "heavy", 10)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR"))

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

	// No conflicts anymore. There was one before the reconciler refactor, because
	// the CR was re-created after the CRD was deleted.
	// The remediator considered it as an update event. When it sent the update
	// call, it failed with a ConflictUpdateDoesNotExist error, so a conflict
	// metric was recorded.
	// Logs from previous implementation:
	// I1030 07:05:19.420371       1 filteredwatcher.go:426] Handling watch event DELETED acme.com/v1, Kind=Anvil
	// I1030 07:05:19.420400       1 filteredwatcher.go:466] Received watch event for object: "Anvil.acme.com, foo/heavy" (rv: 60430537)
	// I1030 07:05:19.420439       1 filteredwatcher.go:478] Received watch event for deleted object "Anvil.acme.com, foo/heavy" (rv: 60430537)
	// I1030 07:05:19.420458       1 queue.go:136] ObjectQueue.Add: Anvil.acme.com, foo/heavy (rv: 60430537)
	// I1030 07:05:19.421500       1 queue.go:224] ObjectQueue.Get: returning object: Anvil.acme.com, foo/heavy (rv: 60430537)
	// I1030 07:05:19.421542       1 worker.go:93] Worker processing object "Anvil.acme.com, foo/heavy" (rv: 60430537)
	// I1030 07:05:19.421587       1 reconciler.go:121] Remediator creating object: Anvil.acme.com, foo/heavy
	// I1030 07:05:19.440319       1 filteredwatcher.go:426] Handling watch event ADDED acme.com/v1, Kind=Anvil
	// I1030 07:05:19.440376       1 filteredwatcher.go:466] Received watch event for object: "Anvil.acme.com, foo/heavy" (rv: 60430539)
	// I1030 07:05:19.440419       1 filteredwatcher.go:482] Received watch event for created/updated object "Anvil.acme.com, foo/heavy" (rv: 60430539)
	// I1030 07:05:19.440442       1 queue.go:136] ObjectQueue.Add: Anvil.acme.com, foo/heavy (rv: 60430539): apiVersion: acme.com/v1
	// I1030 07:05:19.441679       1 apply.go:130] Created object acme.com_anvil_foo_heavy
	// I1030 07:05:19.441738       1 worker.go:125] Worker reconciled "Anvil.acme.com, foo/heavy", rv 60430537
	// I1030 07:05:19.441765       1 queue.go:255] ObjectQueue.Forget: forget object from queue: Anvil.acme.com, foo/heavy (rv: 60430537)
	// I1030 07:05:19.441792       1 queue.go:240] ObjectQueue.Done: retaining object for retry: Anvil.acme.com, foo/heavy (rv: 60430537)
	// I1030 07:05:19.441806       1 worker.go:84] Worker waiting for new object...
	// I1030 07:05:19.441835       1 queue.go:224] ObjectQueue.Get: returning object: Anvil.acme.com, foo/heavy (rv: 60430539)
	// I1030 07:05:19.441874       1 worker.go:93] Worker processing object "Anvil.acme.com, foo/heavy" (rv: 60430539)
	// I1030 07:05:19.441984       1 reconciler.go:132] Remediator updating object: Anvil.acme.com, foo/heavy (declared: , actual: 60430539)
	// I1030 07:05:19.445236       1 reconciler.go:97] KNV2008: tried to update resource which does not exist: the server could not find the requested resource (patch anvils.acme.com heavy)
	//
	// After the refactor, the first remediation for the DELETE event failed with
	// a KNV2010 error, so the deleted object was never created successfully, so
	// there is no update conflict.
	// Logs from the new implementation:
	// I1030 07:24:58.079209       1 filteredwatcher.go:425] Handling watch event DELETED acme.com/v1, Kind=Anvil
	// I1030 07:24:58.079248       1 filteredwatcher.go:465] Received watch event for object: "Anvil.acme.com, foo/heavy" (generation: 1)
	// I1030 07:24:58.079326       1 filteredwatcher.go:477] Received watch event for deleted object "Anvil.acme.com, foo/heavy" (generation: 1)
	// I1030 07:24:58.079345       1 remediate_resources.go:85] ObjectQueue.Add: Anvil.acme.com, foo/heavy (generation: 1)
	// I1030 07:24:58.989343       1 remediate_resources.go:116] ObjectQueue.Enqueue: Anvil.acme.com, foo/heavy
	// I1030 07:24:58.989468       1 remediator.go:79] New remediator reconciliation for "Anvil.acme.com, foo/heavy"
	// I1030 07:24:58.989537       1 remediator.go:141] Remediator processing object "Anvil.acme.com, foo/heavy" (generation: 1)
	// I1030 07:24:58.989652       1 remediator.go:273] Remediator creating object: Anvil.acme.com, foo/heavy
	// I1030 07:24:58.992243       1 apply.go:124] Failed to create object acme.com_anvil_foo_heavy: KNV2010: unable to apply resource: the server could not find the requested resource (patch anvils.acme.com heavy)

	// Reset discovery client to invalidate the cached Anvil CRD
	nt.RenewClient()

	// Modify the Anvil yaml to trigger immediate re-sync, instead of waiting
	// for automatic retry (1hr default).
	anvilObj = anvilCR("v1", "heavy", 100)
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilObj))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Modify Anvil CR"))
	secondCommitHash := nt.RootRepos[configsync.RootSyncName].MustHash(nt.T)

	nt.WaitForRootSyncSourceError(configsync.RootSyncName, status.UnknownKindErrorCode, "")

	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncNN)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Validate the source error metric was recorded.
	// No management conflict is expected for this new commit, because the
	// remediator should be paused waiting for the sync to succeed.
	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, secondCommitHash, metrics.ErrorSummary{
			Source: 1,
		}))
	if err != nil {
		nt.T.Fatal(err)
	}

	// Remove the CR.
	// This should fix the error and the conflict.
	nt.Must(nt.RootRepos[configsync.RootSyncName].Remove("acme/namespaces/foo/anvil-v1.yaml"))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Removing the Anvil CR as well"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// CR wasn't added to the inventory, so it won't be deleted.
	nt.MetricsExpectations.RemoveObject(configsync.RootSyncKind, rootSyncNN, anvilObj)

	// Validate reconciler error and conflict are cleared.
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

	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/ns.yaml", fake.NamespaceObject("foo")))
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 10)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding Anvil CR"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	nt.RenewClient()

	err = nt.Validate("heavy", "foo", anvilCR("v1", "", 0), weightEqual10)
	if err != nil {
		nt.T.Fatal(err)
	}

	// Update CustomResource
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/namespaces/foo/anvil-v1.yaml", anvilCR("v1", "heavy", 100)))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Updating Anvil CR"))
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
