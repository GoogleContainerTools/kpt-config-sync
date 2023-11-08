// Copyright 2023 Google LLC
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

package testresourcegroup

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/resourcegroup/apis/kpt.dev/v1alpha1"
	"kpt.dev/resourcegroup/controllers/resourcegroup"
	"kpt.dev/resourcegroup/controllers/status"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// NotOwnedMessage is the status message for a resource which does not have
	// an inventory id.
	NotOwnedMessage = "This object is not owned by any inventory object. The status for the current object may not reflect the specification for it in current ResourceGroup."
)

// New creates a new ResourceGroup object
func New(nn types.NamespacedName, id string) *v1alpha1.ResourceGroup {
	rg := &v1alpha1.ResourceGroup{}
	rg.SetNamespace(nn.Namespace)
	rg.SetName(nn.Name)
	if id != "" {
		rg.SetLabels(map[string]string{
			common.InventoryLabel: id,
		})
	}
	return rg
}

// GenerateResourceStatus is a helper function to generate an array of ResourceStatus
// from ObjMetadata
func GenerateResourceStatus(resources []v1alpha1.ObjMetadata, s v1alpha1.Status, sourceHash string) []v1alpha1.ResourceStatus {
	resourceStatus := []v1alpha1.ResourceStatus{}
	for _, resource := range resources {
		resourceStatus = append(resourceStatus, v1alpha1.ResourceStatus{
			ObjMetadata: resource,
			Status:      s,
			SourceHash:  sourceHash,
		})
	}
	return resourceStatus
}

// EmptyStatus returns the initial/default ResourceStatus of a
// ResourceGroup object. This is the status set by the controller upon a successful
// reconcile.
func EmptyStatus() v1alpha1.ResourceGroupStatus {
	return v1alpha1.ResourceGroupStatus{
		Conditions: []v1alpha1.Condition{
			{
				Type:    v1alpha1.Reconciling,
				Status:  v1alpha1.FalseConditionStatus,
				Reason:  resourcegroup.FinishReconciling,
				Message: "finish reconciling",
			},
			{
				Type:    v1alpha1.Stalled,
				Status:  v1alpha1.FalseConditionStatus,
				Reason:  resourcegroup.FinishReconciling,
				Message: "finish reconciling",
			},
		},
	}
}

// UpdateResourceGroup modifies a ResourceGroup object on the cluster using
// the provided mutateFn.
func UpdateResourceGroup(kubeClient *testkubeclient.KubeClient, nn types.NamespacedName, mutateFn func(group *v1alpha1.ResourceGroup)) error {
	obj := &v1alpha1.ResourceGroup{}
	if err := kubeClient.Get(nn.Name, nn.Namespace, obj); err != nil {
		return err
	}
	mutateFn(obj)
	return kubeClient.Update(obj)
}

// CreateOrUpdateResources applies the list of resources to the cluster with the provided
// ResourceGroup inventory ID.
func CreateOrUpdateResources(kubeClient *testkubeclient.KubeClient, resources []v1alpha1.ObjMetadata, id string) error {
	for _, r := range resources {
		u := &unstructured.Unstructured{}
		u.SetName(r.Name)
		u.SetNamespace(r.Namespace)
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   r.Group,
			Version: "v1",
			Kind:    r.Kind,
		})
		u.SetAnnotations(map[string]string{
			"config.k8s.io/owning-inventory": id,
			status.SourceHashAnnotationKey:   "1234567890",
		})

		err := kubeClient.Get(r.Name, r.Namespace, u.DeepCopy())
		if err == nil {
			if updateErr := kubeClient.Update(u); updateErr != nil {
				return updateErr
			}
		} else if apierrors.IsNotFound(err) {
			if createErr := kubeClient.Create(u); createErr != nil {
				return createErr
			}
		} else {
			return err
		}
	}
	return nil
}

// ValidateNoControllerPodRestarts checks that no pods have restarted in the ResourceGroup
// controller namespace.
func ValidateNoControllerPodRestarts(kubeClient *testkubeclient.KubeClient) error {
	podList := &v1.PodList{}
	opts := []client.ListOption{
		client.InNamespace(configmanagement.RGControllerNamespace),
	}
	if err := kubeClient.List(podList, opts...); err != nil {
		return err
	}
	for _, pod := range podList.Items {
		for _, c := range pod.Status.ContainerStatuses {
			if c.RestartCount != 0 {
				return fmt.Errorf("%s has a restart as %d", c.Name, c.RestartCount)
			}
		}
	}
	return nil
}
