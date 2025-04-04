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
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// UnstructuredInventoryConverter is used for converting ResourceGroup objects and persisting
// Config Sync specific metadata.
// This provides the translation logic to instantiate a UnstructuredClient which
// works with ResourceGroup objects.
type UnstructuredInventoryConverter struct {
	syncKind      string
	syncName      string
	syncNamespace string
	statusMode    metadata.StatusMode
	// TODO: add source/commit hash and interface method once it's to be persisted on ResourceGroup
}

// NewInventoryConverter constructs a new UnstructuredInventoryConverter
func NewInventoryConverter(scope declared.Scope, syncName string, statusMode metadata.StatusMode) *UnstructuredInventoryConverter {
	return &UnstructuredInventoryConverter{
		syncKind:      scope.SyncKind(),
		syncName:      syncName,
		syncNamespace: scope.SyncNamespace(),
		statusMode:    statusMode,
	}
}

// InventoryFromUnstructured converts a ResourceGroup object to UnstructuredInventory
func (ic *UnstructuredInventoryConverter) InventoryFromUnstructured(fromObj *unstructured.Unstructured) (*inventory.UnstructuredInventory, error) {
	if fromObj == nil {
		return nil, fmt.Errorf("unstructured ResourceGroup object is nil")
	}
	klog.V(4).Infof("converting ResourceGroup to UnstructuredInventory")
	unstructuredInventory := inventory.NewUnstructuredInventory(fromObj)
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

// InventoryToUnstructured converts a UnstructuredInventory to a ResourceGroup
// object, using the current ResourceGroup object as a base.
func (ic *UnstructuredInventoryConverter) InventoryToUnstructured(fromObj *unstructured.Unstructured, toInv *inventory.UnstructuredInventory) (*unstructured.Unstructured, error) {
	if fromObj == nil {
		return nil, fmt.Errorf("unstructured object is nil")
	}
	if toInv == nil {
		return nil, fmt.Errorf("UnstructuredInventory object is nil")
	}
	klog.V(4).Infof("converting UnstructuredInventory to ResourceGroup object")
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

	var objStatus []interface{}
	if ic.statusMode == metadata.StatusEnabled {
		objStatusMap := map[object.ObjMetadata]actuation.ObjectStatus{}
		for _, s := range toInv.GetObjectStatuses() {
			objStatusMap[inventory.ObjMetadataFromObjectReference(s.ObjectReference)] = s
		}
		klog.V(4).Infof("Creating list of %d resource statuses", len(toInv.GetObjectRefs()))
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
	}

	toObj := fromObj.DeepCopy()
	// Set Config Sync specific metadata on the object
	core.SetLabel(toObj, metadata.ManagedByKey, metadata.ManagedByValue)
	core.SetLabel(toObj, metadata.SyncNamespaceLabel, ic.syncNamespace)
	core.SetLabel(toObj, metadata.SyncNameLabel, ic.syncName)
	core.SetLabel(toObj, metadata.SyncKindLabel, ic.syncKind)
	core.SetAnnotation(toObj, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
	core.SetAnnotation(toObj, metadata.StatusModeAnnotationKey, ic.statusMode.String())

	if len(objs) == 0 { // Unset resources
		klog.V(4).Infoln("clearing inventory resources")
		unstructured.RemoveNestedField(toObj.Object,
			"spec", "resources")
	} else { // Update resources
		klog.V(4).Infof("storing inventory (%d) resources", len(objs))
		err := unstructured.SetNestedSlice(toObj.Object,
			objs, "spec", "resources")
		if err != nil {
			return nil, err
		}
	}
	if len(objStatus) == 0 { // Unset resource statuses
		klog.V(4).Infoln("clearing inventory resourceStatuses")
		unstructured.RemoveNestedField(toObj.Object,
			"status", "resourceStatuses")
	} else { // Update resource statuses
		klog.V(4).Infof("storing inventory (%d) resourceStatuses", len(objStatus))
		err := unstructured.SetNestedSlice(toObj.Object,
			objStatus, "status", "resourceStatuses")
		if err != nil {
			return nil, err
		}
	}
	generation := toObj.GetGeneration() // Update status.observedGeneration
	klog.V(4).Infof("setting observedGeneration %d: ", generation)
	err := unstructured.SetNestedField(toObj.Object,
		generation, "status", "observedGeneration")
	if err != nil {
		return nil, err
	}
	return toObj, nil
}
