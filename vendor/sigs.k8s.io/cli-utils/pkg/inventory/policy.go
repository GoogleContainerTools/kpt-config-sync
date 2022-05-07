// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
)

// Policy defines if an inventory object can take over
// objects that belong to another inventory object or don't
// belong to any inventory object.
// This is done by determining if the apply/prune operation
// can go through for a resource based on the comparison
// the inventory-id value in the package and the owning-inventory
// annotation in the live object.
//go:generate stringer -type=Policy -linecomment
type Policy int

const (
	// PolicyMustMatch: This policy enforces that the resources being applied can not
	// have any overlap with objects in other inventories or objects that already exist
	// in the cluster but don't belong to an inventory.
	//
	// The apply operation can go through when
	// - A new resources in the package doesn't exist in the cluster
	// - An existing resource in the package doesn't exist in the cluster
	// - An existing resource exist in the cluster. The owning-inventory annotation in the live object
	//   matches with that in the package.
	//
	// The prune operation can go through when
	// - The owning-inventory annotation in the live object match with that
	//   in the package.
	PolicyMustMatch Policy = iota // MustMatch

	// PolicyAdoptIfNoInventory: This policy enforces that resources being applied
	// can not have any overlap with objects in other inventories, but are
	// permitted to take ownership of objects that don't belong to any inventories.
	//
	// The apply operation can go through when
	// - New resource in the package doesn't exist in the cluster
	// - If a new resource exist in the cluster, its owning-inventory annotation is empty
	// - Existing resource in the package doesn't exist in the cluster
	// - If existing resource exist in the cluster, its owning-inventory annotation in the live object
	//   is empty
	// - An existing resource exist in the cluster. The owning-inventory annotation in the live object
	//   matches with that in the package.
	//
	// The prune operation can go through when
	// - The owning-inventory annotation in the live object match with that
	//   in the package.
	// - The live object doesn't have the owning-inventory annotation.
	PolicyAdoptIfNoInventory // AdoptIfNoInventory

	// PolicyAdoptAll: This policy will let the current inventory take ownership of any objects.
	//
	// The apply operation can go through for any resource in the package even if the
	// live object has an unmatched owning-inventory annotation.
	//
	// The prune operation can go through when
	// - The owning-inventory annotation in the live object match or doesn't match with that
	//   in the package.
	// - The live object doesn't have the owning-inventory annotation.
	PolicyAdoptAll // AdoptAll
)

// OwningInventoryKey is the annotation key indicating the inventory owning an object.
const OwningInventoryKey = "config.k8s.io/owning-inventory"

// IDMatchStatus represents the result of comparing the
// id from current inventory info and the inventory-id from a live object.
//go:generate stringer -type=IDMatchStatus
type IDMatchStatus int

const (
	Empty IDMatchStatus = iota
	Match
	NoMatch
)

func IDMatch(inv Info, obj *unstructured.Unstructured) IDMatchStatus {
	annotations := obj.GetAnnotations()
	value, found := annotations[OwningInventoryKey]
	if !found {
		return Empty
	}
	if value == inv.ID() {
		return Match
	}
	return NoMatch
}

func CanApply(inv Info, obj *unstructured.Unstructured, policy Policy) (bool, error) {
	matchStatus := IDMatch(inv, obj)
	switch matchStatus {
	case Empty:
		if policy != PolicyMustMatch {
			return true, nil
		}
	case Match:
		return true, nil
	case NoMatch:
		if policy == PolicyAdoptAll {
			return true, nil
		}
	default:
		return false, fmt.Errorf("invalid inventory policy: %v", policy)
	}
	return false, &PolicyPreventedActuationError{
		Strategy: actuation.ActuationStrategyApply,
		Policy:   policy,
		Status:   matchStatus,
	}
}

func CanPrune(inv Info, obj *unstructured.Unstructured, policy Policy) (bool, error) {
	matchStatus := IDMatch(inv, obj)
	switch matchStatus {
	case Empty:
		if policy == PolicyAdoptIfNoInventory || policy == PolicyAdoptAll {
			return true, nil
		}
	case Match:
		return true, nil
	case NoMatch:
		if policy == PolicyAdoptAll {
			return true, nil
		}
	default:
		return false, fmt.Errorf("invalid inventory policy: %v", policy)
	}
	return false, &PolicyPreventedActuationError{
		Strategy: actuation.ActuationStrategyDelete,
		Policy:   policy,
		Status:   matchStatus,
	}
}

func AddInventoryIDAnnotation(obj *unstructured.Unstructured, inv Info) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations[OwningInventoryKey] = inv.ID()
	obj.SetAnnotations(annotations)
}
