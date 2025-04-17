// Copyright 2025 Google LLC
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

package inventory

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/util"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// ResourceGroupInventoryConverter is used for converting ResourceGroup objects and persisting
// Config Sync specific metadata.
// This provides the translation logic to instantiate a UnstructuredClient which
// works with ResourceGroup objects.
type ResourceGroupInventoryConverter struct {
	syncKind      string
	syncName      string
	syncNamespace string
	statusMode    metadata.StatusMode
	// TODO: add source/commit hash and interface method once it's to be persisted on ResourceGroup
}

// NewInventoryConverter constructs a new ResourceGroupInventoryConverter
func NewInventoryConverter(scope declared.Scope, syncName string, statusMode metadata.StatusMode) *ResourceGroupInventoryConverter {
	return &ResourceGroupInventoryConverter{
		syncKind:      scope.SyncKind(),
		syncName:      syncName,
		syncNamespace: scope.SyncNamespace(),
		statusMode:    statusMode,
	}
}

// UnstructuredClientFromFactory builds a new UnstructuredClient from a kubectl
// client factory.
func (ic *ResourceGroupInventoryConverter) UnstructuredClientFromFactory(factory util.Factory) (*inventory.UnstructuredClient, error) {
	var toStatus inventory.ToUnstructuredFunc
	if ic.statusMode == metadata.StatusEnabled {
		klog.Infof("Enabled status reporting")
		toStatus = ic.InventoryToUnstructuredStatus
	} else {
		klog.Infof("Disabled status reporting")
		// Nil tells the UnstructuredClient to skip calling UpdateStatus.
		// This requires Config Sync to remove the status, but avoids
		// unnecessary API calls when status is disabled.
	}
	return inventory.NewUnstructuredClient(factory,
		ic.InventoryFromUnstructured,
		ic.InventoryToUnstructured,
		toStatus,
		v1alpha1.SchemeGroupVersionKind())
}

// InventoryFromUnstructured converts a ResourceGroup object to SingleObjectInventory
func (ic *ResourceGroupInventoryConverter) InventoryFromUnstructured(fromObj *unstructured.Unstructured) (*inventory.SingleObjectInventory, error) {
	if fromObj == nil {
		return nil, fmt.Errorf("unstructured ResourceGroup object is nil")
	}
	klog.V(4).Infof("converting ResourceGroup to SingleObjectInventory")
	unstructuredInventory := inventory.NewSingleObjectInventory(fromObj)
	resources, exists, err := unstructured.NestedSlice(fromObj.Object, "spec", "resources")
	if err != nil {
		return nil, err
	}
	if !exists {
		klog.V(4).Infof("Inventory (spec.resources) is empty")
		return unstructuredInventory, nil
	}
	klog.V(4).Infof("processing %d inventory items", len(resources))
	for _, r := range resources {
		resource := r.(map[string]interface{})
		namespace, _, err := unstructured.NestedString(resource, "namespace")
		if err != nil {
			return nil, err
		}
		name, _, err := unstructured.NestedString(resource, "name")
		if err != nil {
			return nil, err
		}
		group, _, err := unstructured.NestedString(resource, "group")
		if err != nil {
			return nil, err
		}
		kind, _, err := unstructured.NestedString(resource, "kind")
		if err != nil {
			return nil, err
		}
		objMetadata := object.ObjMetadata{
			Name:      strings.TrimSpace(name),
			Namespace: strings.TrimSpace(namespace),
			GroupKind: schema.GroupKind{
				Group: strings.TrimSpace(group),
				Kind:  strings.TrimSpace(kind),
			},
		}
		klog.V(4).Infof("converting to ObjMetadata: %s", objMetadata)
		unstructuredInventory.ObjectRefs = append(unstructuredInventory.ObjectRefs, objMetadata)
	}
	return unstructuredInventory, nil
}

// InventoryToUnstructured converts a SingleObjectInventory to a ResourceGroup
// object, using the current ResourceGroup object as a base.
// Only the metadata and spec are populated. The status is left empty, because
// the server ignores it when updating the object.
func (ic *ResourceGroupInventoryConverter) InventoryToUnstructured(fromObj *unstructured.Unstructured, toInv *inventory.SingleObjectInventory) (*unstructured.Unstructured, error) {
	if fromObj == nil {
		return nil, fmt.Errorf("unstructured object is nil")
	}
	if toInv == nil {
		return nil, fmt.Errorf("SingleObjectInventory object is nil")
	}

	klog.V(4).Infof("converting SingleObjectInventory to ResourceGroup object")
	klog.V(4).Infof("creating list of %d resources", len(toInv.GetObjectRefs()))
	var objs []interface{}
	for _, objMeta := range toInv.GetObjectRefs() {
		klog.V(4).Infof("converting to object reference: %s", objMeta)
		objs = append(objs, map[string]interface{}{
			"group":     objMeta.GroupKind.Group,
			"kind":      objMeta.GroupKind.Kind,
			"namespace": objMeta.Namespace,
			"name":      objMeta.Name,
		})
	}

	toObj := fromObj.DeepCopy()

	// Set Config Sync specific metadata on the object
	ic.setMeta(toObj)

	if len(objs) == 0 {
		// Unset spec.resources
		klog.V(4).Infoln("clearing inventory resources")
		unstructured.RemoveNestedField(toObj.Object,
			"spec", "resources")
	} else {
		// Update spec.resources
		klog.V(4).Infof("storing inventory (%d) resources", len(objs))
		err := unstructured.SetNestedSlice(toObj.Object,
			objs, "spec", "resources")
		if err != nil {
			return nil, err
		}
	}

	// Unset status
	klog.V(4).Infoln("clearing inventory status")
	unstructured.RemoveNestedField(toObj.Object, "status")

	return toObj, nil
}

// InventoryToUnstructuredStatus converts a SingleObjectInventory to a
// ResourceGroup object, using the current ResourceGroup object as a base.
// Only the metadata and status are populated. The spec is left empty, because
// the server ignores it when updating the status.
func (ic *ResourceGroupInventoryConverter) InventoryToUnstructuredStatus(fromObj *unstructured.Unstructured, toInv *inventory.SingleObjectInventory) (*unstructured.Unstructured, error) {
	if fromObj == nil {
		return nil, fmt.Errorf("unstructured object is nil")
	}
	if toInv == nil {
		return nil, fmt.Errorf("SingleObjectInventory object is nil")
	}
	if ic.statusMode != metadata.StatusEnabled {
		// Should never happen.
		return nil, fmt.Errorf("InventoryToUnstructuredStatus must not be called when status is disabled")
	}

	klog.V(4).Infof("converting SingleObjectInventory to ResourceGroup object status")
	var objStatus []interface{}
	objStatusMap := map[object.ObjMetadata]actuation.ObjectStatus{}
	for _, s := range toInv.GetObjectStatuses() {
		objStatusMap[inventory.ObjMetadataFromObjectReference(s.ObjectReference)] = s
	}
	klog.V(4).Infof("Creating list of %d resource statuses", len(objStatusMap))
	for _, objMeta := range toInv.GetObjectRefs() {
		status, found := objStatusMap[objMeta]
		if found {
			klog.V(4).Infof("converting to object status: %s", objMeta)
			objStatus = append(objStatus, map[string]interface{}{
				"group":     objMeta.GroupKind.Group,
				"kind":      objMeta.GroupKind.Kind,
				"namespace": objMeta.Namespace,
				"name":      objMeta.Name,
				"status":    string(v1alpha1.Unknown),
				"strategy":  status.Strategy.String(),
				"actuation": status.Actuation.String(),
				"reconcile": status.Reconcile.String(),
			})
		}
	}

	toObj := fromObj.DeepCopy()
	// Set Config Sync specific metadata on the object
	ic.setMeta(toObj)

	// Unset spec
	klog.V(4).Infoln("clearing inventory spec")
	unstructured.RemoveNestedField(toObj.Object, "spec", "descriptor")
	unstructured.RemoveNestedField(toObj.Object, "spec")

	if len(objStatus) == 0 {
		// Unset status.resourceStatuses
		klog.V(4).Infoln("clearing inventory resourceStatuses")
		unstructured.RemoveNestedField(toObj.Object,
			"status", "resourceStatuses")
	} else {
		// Update status.resourceStatuses
		klog.V(4).Infof("storing inventory (%d) resourceStatuses", len(objStatus))
		err := unstructured.SetNestedSlice(toObj.Object,
			objStatus, "status", "resourceStatuses")
		if err != nil {
			return nil, err
		}
	}

	// Update status.observedGeneration
	generation := toObj.GetGeneration()
	klog.V(4).Infof("setting observedGeneration %d: ", generation)
	err := unstructured.SetNestedField(toObj.Object,
		generation, "status", "observedGeneration")
	if err != nil {
		return nil, err
	}

	return toObj, nil
}

// setMeta set Config Sync specific metadata on the inventory object
func (ic *ResourceGroupInventoryConverter) setMeta(toObj *unstructured.Unstructured) {
	core.SetLabel(toObj, metadata.ManagedByKey, metadata.ManagedByValue)
	core.SetLabel(toObj, metadata.SyncNamespaceLabel, ic.syncNamespace)
	core.SetLabel(toObj, metadata.SyncNameLabel, ic.syncName)
	core.SetLabel(toObj, metadata.SyncKindLabel, ic.syncKind)
	core.SetAnnotation(toObj, metadata.ManagementModeAnnotationKey, metadata.ManagementEnabled.String())
	core.SetAnnotation(toObj, metadata.StatusModeAnnotationKey, ic.statusMode.String())
}
