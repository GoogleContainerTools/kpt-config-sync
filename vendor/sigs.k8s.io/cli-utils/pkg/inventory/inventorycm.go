// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Introduces the ConfigMap struct which implements
// the Inventory interface. The ConfigMap wraps a
// ConfigMap resource which stores the set of inventory
// (object metadata).

package inventory

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/object"
)

var ConfigMapGVK = schema.GroupVersionKind{
	Group:   "",
	Kind:    "ConfigMap",
	Version: "v1",
}

// ConfigMapToInventoryObj takes a passed ConfigMap (as a resource.Info),
// wraps it with the ConfigMap and upcasts the wrapper as
// an the Inventory interface.
func ConfigMapToInventoryObj(uObj *unstructured.Unstructured) (Inventory, error) {
	return configMapToInventory(uObj)
}

// ConfigMapToInventoryInfo takes a passed ConfigMap (as a resource.Info),
// wraps it with the ConfigMap and upcasts the wrapper as
// an the Info interface.
func ConfigMapToInventoryInfo(uObj *unstructured.Unstructured) (Info, error) {
	inv := NewUnstructuredInventory(uObj)
	return inv.Info(), nil
}

// buildDataMap converts the inventory to the storage format to be used in a ConfigMap
func buildDataMap(objMetas object.ObjMetadataSet, objStatus []actuation.ObjectStatus) map[string]string {
	objMap := map[string]string{}
	objStatusMap := map[object.ObjMetadata]actuation.ObjectStatus{}
	for _, status := range objStatus {
		objStatusMap[ObjMetadataFromObjectReference(status.ObjectReference)] = status
	}
	for _, objMetadata := range objMetas {
		if status, found := objStatusMap[objMetadata]; found {
			objMap[objMetadata.String()] = stringFrom(status)
		} else {
			// It's possible that the passed in status doesn't any object status
			objMap[objMetadata.String()] = ""
		}
	}
	return objMap
}

var _ FromUnstructuredFunc = configMapToInventory

func configMapToInventory(configMap *unstructured.Unstructured) (*UnstructuredInventory, error) {
	inv := NewUnstructuredInventory(configMap)
	objMap, exists, err := unstructured.NestedStringMap(configMap.Object, "data")
	if err != nil {
		err := fmt.Errorf("error retrieving object metadata from inventory object")
		return nil, err
	}
	if exists {
		for objStr := range objMap {
			obj, err := object.ParseObjMetadata(objStr)
			if err != nil {
				return nil, err
			}
			inv.ObjectRefs = append(inv.ObjectRefs, obj)
		}
	}
	return inv, nil
}

// ConfigMap does not have an actual status, so the object statuses are persisted
// as values in the ConfigMap key/value pairs.
func inventoryToConfigMap(statusPolicy StatusPolicy) ToUnstructuredFunc {
	return func(uObj *unstructured.Unstructured, inv *UnstructuredInventory) (*unstructured.Unstructured, error) {
		var dataMap map[string]string
		if statusPolicy == StatusPolicyAll {
			dataMap = buildDataMap(inv.GetObjectRefs(), inv.GetObjectStatuses())
		} else {
			dataMap = buildDataMap(inv.GetObjectRefs(), nil)
		}
		// Adds the inventory map to the ConfigMap "data" section.
		err := unstructured.SetNestedStringMap(uObj.UnstructuredContent(),
			dataMap, "data")
		if err != nil {
			return nil, err
		}
		return uObj, err
	}
}

func stringFrom(status actuation.ObjectStatus) string {
	tmp := map[string]string{
		"strategy":  status.Strategy.String(),
		"actuation": status.Actuation.String(),
		"reconcile": status.Reconcile.String(),
	}
	data, err := json.Marshal(tmp)
	if err != nil || string(data) == "{}" {
		return ""
	}
	return string(data)
}
