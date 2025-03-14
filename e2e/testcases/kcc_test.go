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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testresourcegroup"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
)

// This file includes tests for KCC resources from a cloud source repository.
// The test applies KCC resources and verifies the GCP resources
// are created successfully.
// It then deletes KCC resources and verifies the GCP resources
// are removed successfully.
func TestKCCResourcesOnCSR(t *testing.T) {
	rootSyncID := nomostest.DefaultRootSyncID
	nt := nomostest.New(t, nomostesting.SyncSource, ntopts.KCCTest, ntopts.RequireGKE(t))

	nt.T.Log("sync to the kcc resources from a CSR repo")
	repo := gitproviders.ReadOnlyRepository{
		URL: fmt.Sprintf("%s/p/%s/r/configsync-kcc", nomostesting.CSRHost, *e2e.GCPProject),
	}
	rs := nt.RootSyncObjectGit(rootSyncID.Name, repo, gitproviders.MainBranch, "", "kcc", configsync.SourceFormatUnstructured)
	rs.Spec.Git.Auth = "gcpserviceaccount"
	rs.Spec.Git.GCPServiceAccountEmail = fmt.Sprintf("e2e-test-csr-reader@%s.iam.gserviceaccount.com", *e2e.GCPProject)
	rs.Spec.Git.SecretRef = nil
	nt.Must(nt.KubeClient.Apply(rs))

	commit, err := nomostest.GitCommitFromSpec(nt, rs.Spec.Git)
	if err != nil {
		nt.T.Fatal(err)
	}
	nomostest.SetExpectedGitCommit(nt, rootSyncID, commit)

	nt.Must(nt.WatchForAllSyncs())

	// Verify that the GCP resources are created.
	gvkPubSubTopic := schema.GroupVersionKind{
		Group:   "pubsub.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "PubSubTopic",
	}
	gvkPubSubSubscription := schema.GroupVersionKind{
		Group:   "pubsub.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "PubSubSubscription",
	}
	gvkServiceAccount := schema.GroupVersionKind{
		Group:   "iam.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "IAMServiceAccount",
	}
	gvkPolicyMember := schema.GroupVersionKind{
		Group:   "iam.cnrm.cloud.google.com",
		Version: "v1beta1",
		Kind:    "IAMPolicyMember",
	}

	// Wait until all objects are reconciled
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkPubSubTopic, "test-cs", "foo",
			testwatcher.WatchUnstructured())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkPubSubSubscription, "test-cs-read", "foo",
			testwatcher.WatchUnstructured())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkServiceAccount, "pubsub-app", "foo",
			testwatcher.WatchUnstructured())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForCurrentStatus(gvkPolicyMember, "policy-member-binding", "foo",
			testwatcher.WatchUnstructured())
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}

	// Remove the kcc resources
	nt.T.Log("sync to an empty directory from a CSR repo")
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kcc-empty"}}}`)
	nomostest.SetExpectedSyncPath(nt, rootSyncID, "kcc-empty")
	nt.Must(nt.WatchForAllSyncs())

	// Wait until all objects are not found
	tg = taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkPubSubTopic, "test-cs", "foo",
			testwatcher.WatchUnstructured())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkPubSubSubscription, "test-cs-read", "foo",
			testwatcher.WatchUnstructured())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkServiceAccount, "pubsub-app", "foo",
			testwatcher.WatchUnstructured())
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(gvkPolicyMember, "policy-member-binding", "foo",
			testwatcher.WatchUnstructured())
	})
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestKCCResourceGroup(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController, ntopts.KCCTest)

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		ns := k8sobjects.NamespaceObject(namespace)
		nt.Must(nt.KubeClient.DeleteIfExists(ns))
	})
	nt.Must(nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)))

	rgNN := types.NamespacedName{
		Name:      "group-kcc",
		Namespace: namespace,
	}
	nt.T.Logf("Create test ResourceGroup: %s", rgNN)
	resourceID := rgNN.Name
	nt.T.Cleanup(func() {
		rg := testresourcegroup.New(rgNN, resourceID)
		nt.Must(nt.KubeClient.DeleteIfExists(rg))
	})
	rg := testresourcegroup.New(rgNN, resourceID)
	nt.Must(nt.KubeClient.Create(rg))

	nt.T.Log("Create a KCC object and verify the ResourceGroup status")
	kccObj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "serviceusage.cnrm.cloud.google.com/v1beta1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name":      "pubsub.googleapis.com",
				"namespace": namespace,
				"annotations": map[string]interface{}{
					"config.k8s.io/owning-inventory": resourceID,
				},
			},
		},
	}
	resource := v1alpha1.ObjMetadata{
		GroupKind: v1alpha1.GroupKind{
			Group: kccObj.GroupVersionKind().Group,
			Kind:  kccObj.GroupVersionKind().Kind,
		},
		Namespace: kccObj.GetNamespace(),
		Name:      kccObj.GetName(),
	}
	nt.Must(nt.KubeClient.Create(kccObj))

	nt.T.Log("Add the KCC object to the ResourceGroup")
	rg.Spec.Resources = []v1alpha1.ObjMetadata{resource}
	nt.Must(nt.KubeClient.Update(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Verifying the resourcegroup status: It can surface the kcc resource status.conditions")
	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 2
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resource,
			Conditions: []v1alpha1.Condition{
				{
					Type:    "Ready",
					Status:  "False",
					Reason:  "UpdateFailed",
					Message: "Update call failed",
				},
			},
			Status: v1alpha1.InProgress,
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))
}
