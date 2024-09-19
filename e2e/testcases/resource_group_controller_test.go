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
	"context"
	"errors"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testresourcegroup"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/resourcegroup"
)

func TestResourceGroupController(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	ns := "rg-test"
	nt.Must(rootSyncGitRepo.Add(
		"acme/namespaces/rg-test/ns.yaml",
		k8sobjects.NamespaceObject(ns)))

	cmName := "e2e-test-configmap"
	cmPath := "acme/namespaces/rg-test/configmap.yaml"
	cm := k8sobjects.ConfigMapObject(core.Name(cmName), core.Namespace(ns))
	nt.Must(rootSyncGitRepo.Add(cmPath, cm))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding a ConfigMap to repo"))
	nt.Must(nt.WatchForAllSyncs())

	// Checking that the ResourceGroup controller captures the status of the
	// managed resources.
	id := applier.InventoryID(configsync.RootSyncName, configsync.ControllerNamespace)
	_, err := retry.Retry(60*time.Second, func() error {
		rg := resourcegroup.Unstructured(configsync.RootSyncName, configsync.ControllerNamespace, id)
		err := nt.Validate(configsync.RootSyncName, configsync.ControllerNamespace, rg,
			testpredicates.AllResourcesReconciled(nt.Scheme))
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestResourceGroupControllerInKptGroup(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		// all test resources are created in this namespace
		if err := nt.KubeClient.Delete(k8sobjects.NamespaceObject(namespace)); err != nil {
			nt.T.Error(err)
		}
	})
	if err := nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)); err != nil {
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

	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 1
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	resources := []v1alpha1.ObjMetadata{
		{
			Name:      "example",
			Namespace: rgNN.Namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
	}
	nt.Must(testresourcegroup.UpdateResourceGroup(nt.KubeClient, rgNN, func(rg *v1alpha1.ResourceGroup) {
		rg.Spec.Resources = resources
	}))

	subRGNN := types.NamespacedName{
		Name:      "group-b",
		Namespace: rgNN.Namespace,
	}
	subRG := testresourcegroup.New(subRGNN, subRGNN.Name)
	if err := nt.KubeClient.Create(subRG); err != nil {
		nt.T.Fatal(err)
	}

	nt.Must(testresourcegroup.UpdateResourceGroup(nt.KubeClient, rgNN, func(rg *v1alpha1.ResourceGroup) {
		rg.Spec.Subgroups = []v1alpha1.GroupMetadata{
			{
				Name:      subRGNN.Name,
				Namespace: subRGNN.Namespace,
			},
		}
	}))

	expectedStatus.ObservedGeneration = 3
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resources[0],
			Status:      v1alpha1.NotFound,
		},
	}
	expectedStatus.SubgroupStatuses = []v1alpha1.GroupStatus{
		{
			GroupMetadata: v1alpha1.GroupMetadata{
				Namespace: subRGNN.Namespace,
				Name:      subRGNN.Name,
			},
			Status: v1alpha1.Current,
			Conditions: []v1alpha1.Condition{
				{
					Type:    v1alpha1.Ownership,
					Status:  v1alpha1.UnknownConditionStatus,
					Reason:  v1alpha1.OwnershipEmpty,
					Message: testresourcegroup.NotOwnedMessage,
				},
			},
		},
	}

	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		), testwatcher.WatchTimeout(time.Minute)))

	if err := testresourcegroup.CreateOrUpdateResources(nt.KubeClient, resources, resourceID); err != nil {
		nt.T.Fatal(err)
	}

	expectedStatus.ObservedGeneration = 3
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resources[0],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
		},
	}

	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		), testwatcher.WatchTimeout(time.Minute)))

	if err := testresourcegroup.CreateOrUpdateResources(nt.KubeClient, resources, "another"); err != nil {
		nt.T.Fatal(err)
	}
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resources[0],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
			Conditions: []v1alpha1.Condition{
				{
					Type:    v1alpha1.Ownership,
					Status:  v1alpha1.TrueConditionStatus,
					Reason:  v1alpha1.OwnershipUnmatch,
					Message: "This resource is owned by another ResourceGroup another. The status only reflects the specification for the current object in ResourceGroup another.",
				},
			},
		},
	}

	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		), testwatcher.WatchTimeout(time.Minute)))

	if err := nt.KubeClient.Delete(rg); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.KubeClient.Delete(subRG); err != nil {
		nt.T.Fatal(err)
	}

	if err := testresourcegroup.ValidateNoControllerPodRestarts(nt.KubeClient); err != nil {
		nt.T.Fatal(err)
	}
}

func TestResourceGroupCustomResource(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		// all test resources are created in this namespace
		if err := nt.KubeClient.Delete(k8sobjects.NamespaceObject(namespace)); err != nil {
			nt.T.Error(err)
		}
	})
	if err := nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)); err != nil {
		nt.T.Fatal(err)
	}
	rgNN := types.NamespacedName{
		Name:      "group-d",
		Namespace: namespace,
	}
	resourceID := rgNN.Name
	rg := testresourcegroup.New(rgNN, resourceID)
	if err := nt.KubeClient.Create(rg); err != nil {
		nt.T.Fatal(err)
	}

	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 1
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))
	crdObj := anvilV1CRD()
	crdGVK := anvilGVK("v1")
	resources := []v1alpha1.ObjMetadata{
		{
			Name:      "example",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: crdGVK.Group,
				Kind:  crdGVK.Kind,
			},
		},
	}
	nt.Must(testresourcegroup.UpdateResourceGroup(nt.KubeClient, rgNN, func(rg *v1alpha1.ResourceGroup) {
		rg.Spec.Resources = resources
	}))
	expectedStatus.ObservedGeneration = 2
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resources[0],
			Status:      v1alpha1.NotFound,
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))
	// Create CRD and CR
	nt.T.Cleanup(func() {
		crdObj := anvilV1CRD()
		if err := nt.KubeClient.Delete(crdObj); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	})
	if err := nt.KubeClient.Create(crdObj); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Watcher.WatchForCurrentStatus(kinds.CustomResourceDefinitionV1(), crdObj.Name, ""); err != nil {
		nt.T.Fatal(err)
	}
	anvilObj := anvilCR("v1", resources[0].Name, 10)
	anvilObj.SetNamespace(resources[0].Namespace)
	nt.T.Cleanup(func() {
		anvilObj := anvilCR("v1", resources[0].Name, 10)
		anvilObj.SetNamespace(resources[0].Namespace)
		if err := nt.KubeClient.Delete(anvilObj); err != nil && !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	})
	if err := nt.KubeClient.Create(anvilObj); err != nil {
		nt.T.Fatal(err)
	}
	// assert status is updated
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resources[0],
			Status:      v1alpha1.Current,
			Conditions: []v1alpha1.Condition{
				{
					Type:    v1alpha1.Ownership,
					Status:  v1alpha1.UnknownConditionStatus,
					Reason:  "Unknown",
					Message: testresourcegroup.NotOwnedMessage,
				},
			},
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))
	if err := nt.KubeClient.Delete(crdObj); err != nil {
		nt.T.Fatal(err)
	}
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resources[0],
			Status:      v1alpha1.NotFound,
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))
}

func TestResourceGroupApplyStatus(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		// all test resources are created in this namespace
		if err := nt.KubeClient.Delete(k8sobjects.NamespaceObject(namespace)); err != nil {
			nt.T.Error(err)
		}
	})
	if err := nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)); err != nil {
		nt.T.Fatal(err)
	}
	rgNN := types.NamespacedName{
		Name:      "group-e",
		Namespace: namespace,
	}
	resourceID := rgNN.Name
	rg := testresourcegroup.New(rgNN, resourceID)
	if err := nt.KubeClient.Create(rg); err != nil {
		nt.T.Fatal(err)
	}

	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 1
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	nt.T.Log("Create and apply ConfigMaps")
	resources := []v1alpha1.ObjMetadata{
		{
			Name:      "test-status-0",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
		{
			Name:      "test-status-1",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
		{
			Name:      "test-status-2",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
		{
			Name:      "test-status-3",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
		{
			Name:      "test-status-4",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
		{
			Name:      "test-status-5",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
		{
			Name:      "test-status-6",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: "",
				Kind:  "ConfigMap",
			},
		},
	}
	nt.T.Log("Apply all but the last ConfigMap to test NotFound error")
	nt.Must(testresourcegroup.CreateOrUpdateResources(nt.KubeClient, resources[:len(resources)-1], resourceID))
	nt.Must(testresourcegroup.UpdateResourceGroup(nt.KubeClient, rgNN, func(rg *v1alpha1.ResourceGroup) {
		rg.Spec.Resources = resources
	}))
	expectedStatus.ObservedGeneration = 2
	expectedStatus.ResourceStatuses = []v1alpha1.ResourceStatus{
		{
			ObjMetadata: resources[0],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
		},
		{
			ObjMetadata: resources[1],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
		},
		{
			ObjMetadata: resources[2],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
		},
		{
			ObjMetadata: resources[3],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
		},
		{
			ObjMetadata: resources[4],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
		},
		{
			ObjMetadata: resources[5],
			Status:      v1alpha1.Current,
			SourceHash:  "1234567",
		},
		{
			ObjMetadata: resources[6],
			Status:      v1alpha1.NotFound,
		},
	}
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	nt.T.Log("inject resource status to verify reconcile behavior")
	nt.Must(testresourcegroup.UpdateResourceGroup(nt.KubeClient, rgNN, func(rg *v1alpha1.ResourceGroup) {
		rg.Status.ResourceStatuses = nil
	}))
	nt.T.Log("verify that the controller reconciles to the original status")
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))
	actualRG := &v1alpha1.ResourceGroup{}
	if err := nt.KubeClient.Get(rgNN.Name, rgNN.Namespace, actualRG); err != nil {
		nt.T.Fatal(err)
	}
	resourceVersion := actualRG.ResourceVersion
	nt.T.Log("Wait and check to see we don't cause an infinite/recursive reconcile loop by ensuring the resourceVersion doesn't change.")
	err := nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceVersionNotEquals(nt.Scheme, resourceVersion),
		), testwatcher.WatchTimeout(60*time.Second))
	if err == nil {
		nt.T.Fatal("expected ResourceGroup ResourceVersion to not change")
	}
	// happy path is a watch timeout, other errors are fatal
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
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
