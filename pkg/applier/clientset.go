// Copyright 2022 Google LLC
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

package applier

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/cmd/util"
	"kpt.dev/configsync/pkg/api/kpt.dev/v1alpha1"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/apply"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/kstatus/watcher"
	"sigs.k8s.io/cli-utils/pkg/object"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// KptApplier is the interface exposed by cli-utils apply.Applier.
// Using an interface, instead of the concrete struct, allows for easier testing.
type KptApplier interface {
	Run(context.Context, inventory.Info, object.UnstructuredSet, apply.ApplierOptions) <-chan event.Event
}

// KptDestroyer is the interface exposed by cli-utils apply.Destroyer.
// Using an interface, instead of the concrete struct, allows for easier testing.
type KptDestroyer interface {
	Run(context.Context, inventory.Info, apply.DestroyerOptions) <-chan event.Event
}

// ClientSet wraps the various Kubernetes clients required for building a
// Config Sync applier.Applier.
type ClientSet struct {
	KptApplier   KptApplier
	KptDestroyer KptDestroyer
	InvClient    inventory.Client
	Client       client.Client
	Mapper       meta.RESTMapper
	StatusMode   string
	ApplySetID   string
}

func inventoryFromUnstructured(obj *unstructured.Unstructured) (*inventory.UnstructuredInventory, error) {
	if obj == nil {
		return nil, fmt.Errorf("unstructured ResourceGroup object is nil")
	}
	unstructuredInventory := &inventory.UnstructuredInventory{
		ClusterObj: obj,
	}
	klog.V(4).Infof("converting ResourceGroup to InternalInventory")
	resources, exists, err := unstructured.NestedSlice(obj.Object, "spec", "resources")
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
			Name:      name,
			Namespace: namespace,
			GroupKind: schema.GroupKind{
				Group: strings.TrimSpace(group),
				Kind:  strings.TrimSpace(kind),
			},
		}
		klog.V(4).Infof("converting to ObjMetadata: %s", objMetadata)
		unstructuredInventory.Objs = append(unstructuredInventory.Objs, objMetadata)
	}
	return unstructuredInventory, nil
}

func inventoryToUnstructured(inv *inventory.UnstructuredInventory) (*unstructured.Unstructured, error) {
	if inv == nil {
		return nil, fmt.Errorf("UnstructuredInventory object is nil")
	}
	objStatusMap := map[object.ObjMetadata]actuation.ObjectStatus{}
	for _, s := range inv.ObjectStatuses() {
		objStatusMap[inventory.ObjMetadataFromObjectReference(s.ObjectReference)] = s
	}
	klog.V(4).Infof("converting UnstructuredInventory to ResourceGroup object")
	klog.V(4).Infof("creating list of %d resources", len(inv.Objects()))
	var objs []interface{}
	for _, objMeta := range inv.Objects() {
		klog.V(4).Infof("converting to object reference: %s", objMeta)
		objs = append(objs, map[string]interface{}{
			"group":     objMeta.GroupKind.Group,
			"kind":      objMeta.GroupKind.Kind,
			"namespace": objMeta.Namespace,
			"name":      objMeta.Name,
		})
	}
	klog.V(4).Infof("Creating list of %d resource statuses", len(inv.Objects()))
	var objStatus []interface{}
	for _, objMeta := range inv.Objects() {
		status, found := objStatusMap[objMeta]
		if found {
			klog.V(4).Infof("converting to object status: %s", objMeta)
			objStatus = append(objStatus, map[string]interface{}{
				"group":     objMeta.GroupKind.Group,
				"kind":      objMeta.GroupKind.Kind,
				"namespace": objMeta.Namespace,
				"name":      objMeta.Name,
				"status":    "Unknown",
				"strategy":  status.Strategy.String(),
				"actuation": status.Actuation.String(),
				"reconcile": status.Reconcile.String(),
			})
		}
	}

	invCopy := inv.ClusterObj.DeepCopy()
	if len(objs) == 0 {
		klog.V(4).Infoln("clearing inventory resources")
		unstructured.RemoveNestedField(invCopy.UnstructuredContent(),
			"spec", "resources")
		unstructured.RemoveNestedField(invCopy.UnstructuredContent(),
			"status", "resourceStatuses")
	} else {
		klog.V(4).Infof("storing inventory (%d) resources", len(objs))
		err := unstructured.SetNestedSlice(invCopy.UnstructuredContent(),
			objs, "spec", "resources")
		if err != nil {
			return nil, err
		}
		err = unstructured.SetNestedSlice(invCopy.UnstructuredContent(),
			objStatus, "status", "resourceStatuses")
		if err != nil {
			return nil, err
		}
		generation := invCopy.GetGeneration()
		klog.V(4).Infof("setting observedGeneration %d: ", generation)
		err = unstructured.SetNestedField(invCopy.UnstructuredContent(),
			generation, "status", "observedGeneration")
		if err != nil {
			return nil, err
		}
	}
	return invCopy, nil
}

// NewClientSet constructs a new ClientSet.
func NewClientSet(c client.Client, configFlags *genericclioptions.ConfigFlags, statusMode, applySetID string) (*ClientSet, error) {
	matchVersionKubeConfigFlags := util.NewMatchVersionFlags(configFlags)
	f := util.NewFactory(matchVersionKubeConfigFlags)

	var statusPolicy inventory.StatusPolicy
	if statusMode == StatusEnabled {
		klog.Infof("Enabled status reporting")
		statusPolicy = inventory.StatusPolicyAll
	} else {
		klog.Infof("Disabled status reporting")
		statusPolicy = inventory.StatusPolicyNone
	}
	invClient, err := inventory.NewUnstructuredClient(f,
		inventoryFromUnstructured, inventoryToUnstructured,
		v1alpha1.SchemeGroupVersionKind(), statusPolicy)
	if err != nil {
		return nil, err
	}

	// Only watch objects applied by this reconciler for status updates.
	// This reduces both the number of events processed and the memory used by
	// the informer cache.
	watchFilters := &watcher.Filters{
		Labels: labels.Set{
			metadata.ApplySetPartOfLabel: applySetID,
		}.AsSelector(),
	}

	applier, err := apply.NewApplierBuilder().
		WithInventoryClient(invClient).
		WithFactory(f).
		WithStatusWatcherFilters(watchFilters).
		Build()
	if err != nil {
		return nil, err
	}

	destroyer, err := apply.NewDestroyerBuilder().
		WithInventoryClient(invClient).
		WithFactory(f).
		WithStatusWatcherFilters(watchFilters).
		Build()
	if err != nil {
		return nil, err
	}

	mapper, err := f.ToRESTMapper()
	if err != nil {
		return nil, err
	}

	return &ClientSet{
		KptApplier:   applier,
		KptDestroyer: destroyer,
		InvClient:    invClient,
		Client:       c,
		Mapper:       mapper,
		StatusMode:   statusMode,
		ApplySetID:   applySetID,
	}, nil
}
