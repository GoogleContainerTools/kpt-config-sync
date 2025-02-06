// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//
// This file contains code for a "inventory" object which
// stores object metadata to keep track of sets of
// resources. This "inventory" object must be a ConfigMap
// and it stores the object metadata in the data field
// of the ConfigMap. By storing metadata from all applied
// objects, we can correctly prune and teardown sets
// of resources.

package inventory

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// The default inventory name stored in the inventory template.
const legacyInvName = "inventory"

// FindInventoryObj returns the "Inventory" object (ConfigMap with
// inventory label) if it exists, or nil if it does not exist.
func FindInventoryObj(objs object.UnstructuredSet) *unstructured.Unstructured {
	for _, obj := range objs {
		if IsInventoryObject(obj) {
			return obj
		}
	}
	return nil
}

// IsInventoryObject returns true if the passed object has the
// inventory label.
func IsInventoryObject(obj *unstructured.Unstructured) bool {
	if obj == nil {
		return false
	}
	inventoryLabel, err := retrieveInventoryLabel(obj)
	if err == nil && len(inventoryLabel) > 0 {
		return true
	}
	return false
}

// retrieveInventoryLabel returns the string value of the InventoryLabel
// for the passed inventory object. Returns error if the passed object is nil or
// is not a inventory object.
func retrieveInventoryLabel(obj *unstructured.Unstructured) (string, error) {
	inventoryLabel, exists := obj.GetLabels()[common.InventoryLabel]
	if !exists {
		return "", fmt.Errorf("inventory label does not exist for inventory object: %s", common.InventoryLabel)
	}
	return inventoryLabel, nil
}

// ValidateNoInventory takes a set of unstructured.Unstructured objects and
// validates that no inventory object is in the input slice.
func ValidateNoInventory(objs object.UnstructuredSet) error {
	invs := make(object.UnstructuredSet, 0)
	for _, obj := range objs {
		if IsInventoryObject(obj) {
			invs = append(invs, obj)
		}
	}
	if len(invs) == 0 {
		return nil
	}
	return &MultipleInventoryObjError{
		InventoryObjectTemplates: invs,
	}
}

// splitUnstructureds takes a set of unstructured.Unstructured objects and
// splits it into one set that contains the inventory object templates and
// another one that contains the remaining resources. Returns an error if there
// there is no inventory object or more than one inventory objects.
func SplitUnstructureds(objs object.UnstructuredSet) (*unstructured.Unstructured, object.UnstructuredSet, error) {
	invs := make(object.UnstructuredSet, 0)
	resources := make(object.UnstructuredSet, 0)
	for _, obj := range objs {
		if IsInventoryObject(obj) {
			invs = append(invs, obj)
		} else {
			resources = append(resources, obj)
		}
	}
	var inv *unstructured.Unstructured
	var err error
	switch len(invs) {
	case 0:
		err = &NoInventoryObjError{}
	case 1:
		inv = invs[0]
	default:
		err = &MultipleInventoryObjError{InventoryObjectTemplates: invs}
	}
	return inv, resources, err
}

// addSuffixToName adds the passed suffix (usually a hash) as a suffix
// to the name of the passed object stored in the Info struct. Returns
// an error if name stored in the object differs from the name in
// the Info struct.
func addSuffixToName(obj *unstructured.Unstructured, suffix string) error {
	suffix = strings.TrimSpace(suffix)
	if len(suffix) == 0 {
		return fmt.Errorf("passed empty suffix")
	}

	name := obj.GetName()
	if name != obj.GetName() {
		return fmt.Errorf("inventory object (%s) and resource.Info (%s) have different names", name, obj.GetName())
	}
	// Error if name already has suffix.
	suffix = "-" + suffix
	if strings.HasSuffix(name, suffix) {
		return fmt.Errorf("name already has suffix: %s", name)
	}
	name += suffix
	obj.SetName(name)

	return nil
}

// fixLegacyInventoryName modifies the inventory name if it is
// the legacy default name (i.e. inventory) by adding a random suffix.
// This fixes a problem where inventory object names collide if
// they are created in the same namespace.
func fixLegacyInventoryName(obj *unstructured.Unstructured) error {
	if obj.GetName() == legacyInvName {
		klog.V(4).Infof("renaming legacy inventory name")
		randomSuffix := common.RandomStr()
		return addSuffixToName(obj, randomSuffix)
	}
	return nil
}
