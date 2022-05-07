// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// InventoryPolicyApplyFilter implements ValidationFilter interface to determine
// if an object should be applied based on the cluster object's inventory id,
// the id for the inventory object, and the inventory policy.
type InventoryPolicyApplyFilter struct {
	Client    dynamic.Interface
	Mapper    meta.RESTMapper
	Inv       inventory.Info
	InvPolicy inventory.Policy
}

// Name returns a filter identifier for logging.
func (ipaf InventoryPolicyApplyFilter) Name() string {
	return "InventoryPolicyApplyFilter"
}

// Filter returns an inventory.PolicyPreventedActuationError if the object
// apply should be skipped.
func (ipaf InventoryPolicyApplyFilter) Filter(obj *unstructured.Unstructured) error {
	// optimization to avoid unnecessary API calls
	if ipaf.InvPolicy == inventory.PolicyAdoptAll {
		return nil
	}
	// Object must be retrieved from the cluster to get the inventory id.
	clusterObj, err := ipaf.getObject(object.UnstructuredToObjMetadata(obj))
	if err != nil {
		if apierrors.IsNotFound(err) {
			// This simply means the object hasn't been created yet.
			return nil
		}
		return NewFatalError(fmt.Errorf("failed to get current object from cluster: %w", err))
	}
	_, err = inventory.CanApply(ipaf.Inv, clusterObj, ipaf.InvPolicy)
	if err != nil {
		return err
	}
	return nil
}

// getObject retrieves the passed object from the cluster, or an error if one occurred.
func (ipaf InventoryPolicyApplyFilter) getObject(id object.ObjMetadata) (*unstructured.Unstructured, error) {
	mapping, err := ipaf.Mapper.RESTMapping(id.GroupKind)
	if err != nil {
		return nil, err
	}
	namespacedClient, err := ipaf.Client.Resource(mapping.Resource).Namespace(id.Namespace), nil
	if err != nil {
		return nil, err
	}
	return namespacedClient.Get(context.TODO(), id.Name, metav1.GetOptions{})
}
