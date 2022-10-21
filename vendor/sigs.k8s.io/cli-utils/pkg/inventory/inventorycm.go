// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0
//
// Introduces the ConfigMap struct which implements
// the Inventory interface. The ConfigMap wraps a
// ConfigMap resource which stores the set of inventory
// (object metadata).

package inventory

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
)

var ConfigMapGVK = schema.GroupVersionKind{
	Group:   "",
	Kind:    "ConfigMap",
	Version: "v1",
}

// WrapInventoryObj takes a passed ConfigMap (as a resource.Info),
// wraps it with the ConfigMap and upcasts the wrapper as
// an the Inventory interface.
func WrapInventoryObj(inv *unstructured.Unstructured) Storage {
	return &ConfigMap{inv: inv}
}

// WrapInventoryInfoObj takes a passed ConfigMap (as a resource.Info),
// wraps it with the ConfigMap and upcasts the wrapper as
// an the Info interface.
func WrapInventoryInfoObj(inv *unstructured.Unstructured) Info {
	return &ConfigMap{inv: inv}
}

func InvInfoToConfigMap(inv Info) *unstructured.Unstructured {
	icm, ok := inv.(*ConfigMap)
	if ok {
		return icm.inv
	}
	return nil
}

// ConfigMap wraps a ConfigMap resource and implements
// the Inventory interface. This wrapper loads and stores the
// object metadata (inventory) to and from the wrapped ConfigMap.
type ConfigMap struct {
	inv       *unstructured.Unstructured
	objMetas  object.ObjMetadataSet
	objStatus []actuation.ObjectStatus
}

var _ Info = &ConfigMap{}
var _ Storage = &ConfigMap{}

func (icm *ConfigMap) Name() string {
	return icm.inv.GetName()
}

func (icm *ConfigMap) Namespace() string {
	return icm.inv.GetNamespace()
}

func (icm *ConfigMap) ID() string {
	// Empty string if not set.
	return icm.inv.GetLabels()[common.InventoryLabel]
}

func (icm *ConfigMap) Strategy() Strategy {
	return LabelStrategy
}

func (icm *ConfigMap) UnstructuredInventory() *unstructured.Unstructured {
	return icm.inv
}

// Load is an Inventory interface function returning the set of
// object metadata from the wrapped ConfigMap, or an error.
func (icm *ConfigMap) Load() (object.ObjMetadataSet, error) {
	objs := object.ObjMetadataSet{}
	objMap, exists, err := unstructured.NestedStringMap(icm.inv.Object, "data")
	if err != nil {
		err := fmt.Errorf("error retrieving object metadata from inventory object")
		return objs, err
	}
	if exists {
		for objStr := range objMap {
			obj, err := object.ParseObjMetadata(objStr)
			if err != nil {
				return objs, err
			}
			objs = append(objs, obj)
		}
	}
	return objs, nil
}

// Store is an Inventory interface function implemented to store
// the object metadata in the wrapped ConfigMap. Actual storing
// happens in "GetObject".
func (icm *ConfigMap) Store(objMetas object.ObjMetadataSet, status []actuation.ObjectStatus) error {
	icm.objMetas = objMetas
	icm.objStatus = status
	return nil
}

// GetObject returns the wrapped object (ConfigMap) as a resource.Info
// or an error if one occurs.
func (icm *ConfigMap) GetObject() (*unstructured.Unstructured, error) {
	// Create the objMap of all the resources, and compute the hash.
	objMap := buildObjMap(icm.objMetas, icm.objStatus)
	// Create the inventory object by copying the template.
	invCopy := icm.inv.DeepCopy()
	// Adds the inventory map to the ConfigMap "data" section.
	err := unstructured.SetNestedStringMap(invCopy.UnstructuredContent(),
		objMap, "data")
	if err != nil {
		return nil, err
	}
	return invCopy, nil
}

// Apply is an Storage interface function implemented to apply the inventory
// object. StatusPolicy is not needed since ConfigMaps do not have a status subresource.
func (icm *ConfigMap) Apply(dc dynamic.Interface, mapper meta.RESTMapper, _ StatusPolicy) error {
	invInfo, namespacedClient, err := icm.getNamespacedClient(dc, mapper)
	if err != nil {
		return err
	}

	// Get cluster object, if exsists.
	clusterObj, err := namespacedClient.Get(context.TODO(), invInfo.GetName(), metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	// Create cluster inventory object, if it does not exist on cluster.
	if clusterObj == nil {
		klog.V(4).Infof("creating inventory object: %s/%s", invInfo.GetNamespace(), invInfo.GetName())
		_, err = namespacedClient.Create(context.TODO(), invInfo, metav1.CreateOptions{})
		return err
	}

	// Update the cluster inventory object instead.
	klog.V(4).Infof("updating inventory object: %s/%s", invInfo.GetNamespace(), invInfo.GetName())
	_, err = namespacedClient.Update(context.TODO(), invInfo, metav1.UpdateOptions{})
	return err
}

// ApplyWithPrune is a Storage interface function implemented to apply the inventory object with a list of objects
// to be pruned. StatusPolicy is not needed since ConfigMaps do not have a status subresource.
func (icm *ConfigMap) ApplyWithPrune(dc dynamic.Interface, mapper meta.RESTMapper, _ StatusPolicy, _ object.ObjMetadataSet) error {
	invInfo, namespacedClient, err := icm.getNamespacedClient(dc, mapper)
	if err != nil {
		return err
	}

	// Update the cluster inventory object.
	klog.V(4).Infof("updating inventory object: %s/%s", invInfo.GetNamespace(), invInfo.GetName())
	_, err = namespacedClient.Update(context.TODO(), invInfo, metav1.UpdateOptions{})
	return err
}

// getNamespacedClient is a helper function for Apply and ApplyWithPrune that creates a namespaced client for interacting with the live
// cluster, as well as returning the ConfigMap object as a wrapped resource.Info object.
func (icm *ConfigMap) getNamespacedClient(dc dynamic.Interface, mapper meta.RESTMapper) (*unstructured.Unstructured, dynamic.ResourceInterface, error) {
	invInfo, err := icm.GetObject()
	if err != nil {
		return nil, nil, err
	}
	if invInfo == nil {
		return nil, nil, fmt.Errorf("attempting to create a nil inventory object")
	}

	mapping, err := mapper.RESTMapping(invInfo.GroupVersionKind().GroupKind(), invInfo.GroupVersionKind().Version)
	if err != nil {
		return nil, nil, err
	}

	// Create client to interact with cluster.
	namespacedClient := dc.Resource(mapping.Resource).Namespace(invInfo.GetNamespace())

	return invInfo, namespacedClient, nil
}

func buildObjMap(objMetas object.ObjMetadataSet, objStatus []actuation.ObjectStatus) map[string]string {
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
