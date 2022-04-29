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

// Filter returns true if the passed object should be filtered (NOT applied) and
// a filter reason string; false otherwise. Returns an error if one occurred
// during the filter calculation
func (ipaf InventoryPolicyApplyFilter) Filter(obj *unstructured.Unstructured) (bool, string, error) {
	if obj == nil {
		return true, "missing object", nil
	}
	if ipaf.InvPolicy == inventory.PolicyAdoptAll {
		return false, "", nil
	}
	// Object must be retrieved from the cluster to get the inventory id.
	clusterObj, err := ipaf.getObject(object.UnstructuredToObjMetadata(obj))
	if err != nil {
		if apierrors.IsNotFound(err) {
			// This simply means the object hasn't been created yet.
			return false, "", nil
		}
		return true, "", err
	}
	// Check the inventory id "match" and the adopt policy to determine
	// if an object should be applied.
	canApply, err := inventory.CanApply(ipaf.Inv, clusterObj, ipaf.InvPolicy)
	if !canApply {
		invMatch := inventory.IDMatch(ipaf.Inv, clusterObj)
		reason := fmt.Sprintf("inventory policy prevented apply (inventoryIDMatchStatus: %q, inventoryPolicy: %q)",
			invMatch, ipaf.InvPolicy)
		return true, reason, err
	}
	return false, "", nil
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
