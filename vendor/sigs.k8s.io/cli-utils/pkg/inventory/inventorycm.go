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
	return configMapToInventory(true)(uObj)
}

// ConfigMapToInventoryInfo takes a passed ConfigMap (as a resource.Info),
// wraps it with the ConfigMap and upcasts the wrapper as
// an the Info interface.
func ConfigMapToInventoryInfo(uObj *unstructured.Unstructured) (Info, error) {
	inv := NewSingleObjectInventory(uObj)
	return inv.Info(), nil
}

// buildDataMap converts the inventory to the storage format to be used in a ConfigMap
func buildDataMap(objMetas object.ObjMetadataSet, objStatus object.ObjectStatusSet, statusEnabled bool) (map[string]string, error) {
	objMap := make(map[string]string, len(objMetas))
	var objStatusMap map[object.ObjMetadata]actuation.ObjectStatus
	if statusEnabled {
		objStatusMap = make(map[object.ObjMetadata]actuation.ObjectStatus, len(objStatus))
		for _, status := range objStatus {
			// Copy ObjectStatus to remove ObjectReference
			objStatusMap[ObjMetadataFromObjectReference(status.ObjectReference)] = actuation.ObjectStatus{
				Strategy:  status.Strategy,
				Actuation: status.Actuation,
				Reconcile: status.Reconcile,
			}
		}
	}
	for _, objMetadata := range objMetas {
		if !statusEnabled {
			// Status disabled
			objMap[objMetadata.String()] = ""
		} else if status, found := objStatusMap[objMetadata]; found {
			statusStr, err := formatObjectStatus(status)
			if err != nil {
				return nil, err
			}
			objMap[objMetadata.String()] = statusStr
		} else {
			// Object meta doesn't have a matching object status
			objMap[objMetadata.String()] = ""
		}
	}
	return objMap, nil
}

func parseDataMap(objMap map[string]string, statusEnabled bool) (object.ObjMetadataSet, object.ObjectStatusSet, error) {
	var objRefList object.ObjMetadataSet
	var objStatusList object.ObjectStatusSet
	for objStr, objStatusStr := range objMap {
		objMeta, err := object.ParseObjMetadata(objStr)
		if err != nil {
			return nil, nil, err
		}
		objRefList = append(objRefList, objMeta)
		if statusEnabled {
			objStatus, err := parseObjectStatus(objStatusStr)
			if err != nil {
				return nil, nil, err
			}
			// data map value doesn't include the objRef, so set it from the key
			objStatus.ObjectReference = ObjectReferenceFromObjMetadata(objMeta)
			objStatusList = append(objStatusList, objStatus)
		}
	}
	return objRefList, objStatusList, nil
}

func configMapToInventory(statusEnabled bool) FromUnstructuredFunc {
	return func(configMap *unstructured.Unstructured) (*SingleObjectInventory, error) {
		inv := NewSingleObjectInventory(configMap)
		dataMap, exists, err := unstructured.NestedStringMap(configMap.Object, "data")
		if err != nil {
			return nil, fmt.Errorf("failed to read data field from ConfigMap inventory object: %w", err)
		}
		if exists {
			objRefList, objStatusList, err := parseDataMap(dataMap, statusEnabled)
			if err != nil {
				return nil, fmt.Errorf("failed to parse data field from ConfigMap inventory object: %w", err)
			}
			inv.ObjectRefs = objRefList
			inv.ObjectStatuses = objStatusList
		}
		return inv, nil
	}
}

// ConfigMap does not have an actual status, so the object statuses are persisted
// as values in the ConfigMap key/value pairs.
func inventoryToConfigMap(statusEnabled bool) ToUnstructuredFunc {
	return func(uObj *unstructured.Unstructured, inv *SingleObjectInventory) (*unstructured.Unstructured, error) {
		var dataMap map[string]string
		var err error
		dataMap, err = buildDataMap(inv.GetObjectRefs(), inv.GetObjectStatuses(), statusEnabled)
		if err != nil {
			return nil, err
		}
		// Adds the inventory map to the ConfigMap "data" section.
		err = unstructured.SetNestedStringMap(uObj.UnstructuredContent(),
			dataMap, "data")
		if err != nil {
			return nil, err
		}
		return uObj, err
	}
}

func formatObjectStatus(status actuation.ObjectStatus) (string, error) {
	data, err := json.Marshal(status)
	if err != nil || string(data) == "{}" {
		return "", err
	}
	if string(data) == "{}" {
		return "", nil
	}
	return string(data), nil
}

func parseObjectStatus(statusStr string) (actuation.ObjectStatus, error) {
	var status actuation.ObjectStatus
	if statusStr == "" {
		return status, nil
	}
	if err := json.Unmarshal([]byte(statusStr), &status); err != nil {
		return status, err
	}
	return status, nil
}
