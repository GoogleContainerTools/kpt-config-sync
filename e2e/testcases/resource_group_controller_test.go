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
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testresourcegroup"
	"kpt.dev/configsync/e2e/nomostest/testutils"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/resourcegroup"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestResourceGroupController(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)
	rootSyncGitRepo := nt.SyncSourceGitReadWriteRepository(nomostest.DefaultRootSyncID)

	nt.Must(testutils.UpdateRootSyncReconcilerLogLevel(nt,
		configsync.RootSyncName, reconcilermanager.Reconciler, 5))
	nt.Must(testutils.UpdateDeploymentContainerVerbosityArg(nt,
		configmanagement.RGControllerName, configmanagement.RGControllerNamespace, resourcegroup.ManagerContainerName, 5))

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

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), configsync.RootSyncName, configsync.ControllerNamespace,
		testwatcher.WatchPredicates(
			testpredicates.AllResourcesReconciled(nt.Scheme),
		)))
}

func TestResourceGroupControllerInKptGroup(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	nt.Must(testutils.UpdateRootSyncReconcilerLogLevel(nt,
		configsync.RootSyncName, reconcilermanager.Reconciler, 5))
	nt.Must(testutils.UpdateDeploymentContainerVerbosityArg(nt,
		configmanagement.RGControllerName, configmanagement.RGControllerNamespace, resourcegroup.ManagerContainerName, 5))

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		ns := k8sobjects.NamespaceObject(namespace)
		nt.Must(kubeDeleteIgnoreNotFound(nt, ns))
	})
	nt.Must(nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)))

	nt.T.Log("Create a ResourceGroup")
	rgNN := types.NamespacedName{
		Name:      "group-a",
		Namespace: namespace,
	}
	resourceID := rgNN.Name
	nt.T.Cleanup(func() {
		rg := testresourcegroup.New(rgNN, resourceID)
		nt.Must(kubeDeleteIgnoreNotFound(nt, rg))
	})
	rg := testresourcegroup.New(rgNN, resourceID)
	nt.Must(nt.KubeClient.Create(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 1
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	nt.T.Log("Add a ConfigMap to the ResourceGroup spec")
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
	rg = &v1alpha1.ResourceGroup{}
	nt.Must(nt.KubeClient.Get(rgNN.Name, rgNN.Namespace, rg))
	rg.Spec.Resources = resources
	nt.Must(nt.KubeClient.Update(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.T.Log("Create a ResourceGroup to use as a Subgroup")
	subRGNN := types.NamespacedName{
		Name:      "group-b",
		Namespace: rgNN.Namespace,
	}
	nt.T.Cleanup(func() {
		subRG := testresourcegroup.New(subRGNN, subRGNN.Name)
		nt.Must(kubeDeleteIgnoreNotFound(nt, subRG))
	})
	subRG := testresourcegroup.New(subRGNN, subRGNN.Name)
	nt.Must(nt.KubeClient.Create(subRG))

	nt.T.Log("Add the new ResourceGroup as a Subgroup to the ResourceGroup spec")
	rg = &v1alpha1.ResourceGroup{}
	nt.Must(nt.KubeClient.Get(rgNN.Name, rgNN.Namespace, rg))
	rg.Spec.Subgroups = []v1alpha1.GroupMetadata{
		{
			Name:      subRGNN.Name,
			Namespace: subRGNN.Namespace,
		},
	}
	nt.Must(nt.KubeClient.Update(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.T.Log("Simulate the Reconciler applying the managed resources")
	nt.Must(testresourcegroup.CreateOrUpdateResources(nt.KubeClient, resources, resourceID))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.T.Log("Simulate a different Reconciler adopting the managed resources")
	nt.Must(testresourcegroup.CreateOrUpdateResources(nt.KubeClient, resources, "another"))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.T.Log("Validate that the ResourceGroupController Pods did not crash/exit/restart")
	nt.Must(testresourcegroup.ValidateNoControllerPodRestarts(nt.KubeClient))
}

func TestResourceGroupCustomResource(t *testing.T) {
	nt := nomostest.New(t, nomostesting.ACMController)

	nt.Must(testutils.UpdateRootSyncReconcilerLogLevel(nt,
		configsync.RootSyncName, reconcilermanager.Reconciler, 5))
	nt.Must(testutils.UpdateDeploymentContainerVerbosityArg(nt,
		configmanagement.RGControllerName, configmanagement.RGControllerNamespace, resourcegroup.ManagerContainerName, 5))

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		ns := k8sobjects.NamespaceObject(namespace)
		nt.Must(kubeDeleteIgnoreNotFound(nt, ns))
	})
	nt.Must(nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)))

	nt.T.Log("Create a ResourceGroup")
	rgNN := types.NamespacedName{
		Name:      "group-d",
		Namespace: namespace,
	}
	resourceID := rgNN.Name
	nt.T.Cleanup(func() {
		rg := testresourcegroup.New(rgNN, resourceID)
		nt.Must(kubeDeleteIgnoreNotFound(nt, rg))
	})
	rg := testresourcegroup.New(rgNN, resourceID)
	nt.Must(nt.KubeClient.Create(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 1
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	nt.T.Log("Add an Anvil object to the ResourceGroup spec")
	anvilGVK := newAnvilGVK("v1")
	resources := []v1alpha1.ObjMetadata{
		{
			Name:      "example",
			Namespace: namespace,
			GroupKind: v1alpha1.GroupKind{
				Group: anvilGVK.Group,
				Kind:  anvilGVK.Kind,
			},
		},
	}
	rg = &v1alpha1.ResourceGroup{}
	nt.Must(nt.KubeClient.Get(rgNN.Name, rgNN.Namespace, rg))
	rg.Spec.Resources = resources
	nt.Must(nt.KubeClient.Update(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.T.Log("Create Anvil CRD")
	nt.T.Cleanup(func() {
		crdObj := anvilV1CRD()
		nt.Must(kubeDeleteIgnoreNotFound(nt, crdObj))
	})
	crdObj := anvilV1CRD()
	nt.Must(nt.KubeClient.Create(crdObj))
	nt.Must(nt.Watcher.WatchForCurrentStatus(kinds.CustomResourceDefinitionV1(), crdObj.Name, ""))

	nt.T.Log("Create Anvil object")
	nt.T.Cleanup(func() {
		anvilObj := newAnvilObject("v1", resources[0].Name, 10)
		anvilObj.SetNamespace(resources[0].Namespace)
		nt.Must(kubeDeleteIgnoreNotFound(nt, anvilObj))
	})
	anvilObj := newAnvilObject("v1", resources[0].Name, 10)
	anvilObj.SetNamespace(resources[0].Namespace)
	nt.Must(nt.KubeClient.Create(anvilObj))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.T.Log("Delete the Anvil CRD (k8s should delete all the Anvil objects too)")
	nt.Must(nt.KubeClient.Delete(crdObj))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.Must(testutils.UpdateRootSyncReconcilerLogLevel(nt,
		configsync.RootSyncName, reconcilermanager.Reconciler, 5))
	nt.Must(testutils.UpdateDeploymentContainerVerbosityArg(nt,
		configmanagement.RGControllerName, configmanagement.RGControllerNamespace, resourcegroup.ManagerContainerName, 5))

	namespace := "resourcegroup-e2e"
	nt.T.Cleanup(func() {
		ns := k8sobjects.NamespaceObject(namespace)
		nt.Must(kubeDeleteIgnoreNotFound(nt, ns))
	})
	nt.Must(nt.KubeClient.Create(k8sobjects.NamespaceObject(namespace)))

	nt.T.Log("Create a ResourceGroup")
	rgNN := types.NamespacedName{
		Name:      "group-e",
		Namespace: namespace,
	}
	resourceID := rgNN.Name
	nt.T.Cleanup(func() {
		rg := testresourcegroup.New(rgNN, resourceID)
		nt.Must(kubeDeleteIgnoreNotFound(nt, rg))
	})
	rg := testresourcegroup.New(rgNN, resourceID)
	nt.Must(nt.KubeClient.Create(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
	expectedStatus := testresourcegroup.EmptyStatus()
	expectedStatus.ObservedGeneration = 1
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

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

	nt.T.Log("Update the ResourceGroup spec")
	rg = &v1alpha1.ResourceGroup{}
	nt.Must(nt.KubeClient.Get(rgNN.Name, rgNN.Namespace, rg))
	rg.Spec.Resources = resources
	nt.Must(nt.KubeClient.Update(rg))

	nt.T.Log("Update the ResourceGroup status to simulate the Reconciler")
	rg.Status.ObservedGeneration = rg.Generation
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
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

	nt.T.Log("Update the ResourceGroup to clear resource status")
	rg = &v1alpha1.ResourceGroup{}
	nt.Must(nt.KubeClient.Get(rgNN.Name, rgNN.Namespace, rg))
	rg.Status.ResourceStatuses = nil
	nt.Must(nt.KubeClient.UpdateStatus(rg))

	nt.T.Log("Wait for the ResourceGroupController to update the ResourceGroup status")
	nt.Must(nt.Watcher.WatchObject(kinds.ResourceGroup(), rgNN.Name, rgNN.Namespace,
		testwatcher.WatchPredicates(
			testpredicates.ResourceGroupStatusEquals(expectedStatus),
		)))

	rg = &v1alpha1.ResourceGroup{}
	nt.Must(nt.KubeClient.Get(rgNN.Name, rgNN.Namespace, rg))
	resourceVersion := rg.ResourceVersion

	nt.T.Log("Wait 60s to validate that the ResourceGroupController doesn't make any more updates (no resourceVersion change)")
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

	nt.T.Log("Validate that the ResourceGroupController Pods did not crash/exit/restart")
	nt.Must(testresourcegroup.ValidateNoControllerPodRestarts(nt.KubeClient))
}

func kubeDeleteIgnoreNotFound(nt *nomostest.NT, obj client.Object) error {
	if err := nt.KubeClient.Delete(obj); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	}
	return nil
}
