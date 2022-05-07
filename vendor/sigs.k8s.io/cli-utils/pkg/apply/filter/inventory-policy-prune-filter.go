// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package filter

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/inventory"
)

// InventoryPolicyPruneFilter implements ValidationFilter interface to determine
// if an object should be pruned (deleted) because of the InventoryPolicy
// and if the objects owning inventory identifier matchs the inventory id.
type InventoryPolicyPruneFilter struct {
	Inv       inventory.Info
	InvPolicy inventory.Policy
}

// Name returns a filter identifier for logging.
func (ipf InventoryPolicyPruneFilter) Name() string {
	return "InventoryPolicyFilter"
}

// Filter returns an inventory.PolicyPreventedActuationError if the object
// prune/delete should be skipped.
func (ipf InventoryPolicyPruneFilter) Filter(obj *unstructured.Unstructured) error {
	_, err := inventory.CanPrune(ipf.Inv, obj, ipf.InvPolicy)
	if err != nil {
		return err
	}
	return nil
}
