// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Client expresses an interface for interacting with
// objects which store references to objects (inventory objects).
type Client interface {
	ReadClient
	WriteClient
}

type Inventory interface {
	ID() string
	Objects() object.ObjMetadataSet
	ObjectStatuses() []actuation.ObjectStatus
	SetObjects(object.ObjMetadataSet)
	SetObjectStatuses([]actuation.ObjectStatus)
}

type ReadClient interface {
	Get(ctx context.Context, id Info, opts GetOptions) (Inventory, error)
	List(ctx context.Context, opts ListOptions) ([]Inventory, error)
}

type WriteClient interface {
	Update(ctx context.Context, inv Inventory, opts UpdateOptions) error
	Delete(ctx context.Context, id Info, opts DeleteOptions) error
}

type UpdateOptions struct {
	UpdateStatus bool
}

type GetOptions struct{}

type ListOptions struct{}

type DeleteOptions struct{}

var _ Client = &UnstructuredClient{}

// UnstructuredInventory implements Inventory while also tracking the actual
// KRM object from the cluster. This enables the client to update the
// same object and utilize resourceVersion checks.
type UnstructuredInventory struct {
	BaseInventory
	// ClusterObj represents the KRM which was last fetched from the cluster.
	// used by the client implementation to performs updates on the object.
	ClusterObj *unstructured.Unstructured
}

func (ui *UnstructuredInventory) Name() string {
	if ui.ClusterObj == nil {
		return ""
	}
	return ui.ClusterObj.GetName()
}

func (ui *UnstructuredInventory) Namespace() string {
	if ui.ClusterObj == nil {
		return ""
	}
	return ui.ClusterObj.GetNamespace()
}

func (ui *UnstructuredInventory) ID() string {
	if ui.ClusterObj == nil {
		return ""
	}
	// Empty string if not set.
	return ui.ClusterObj.GetLabels()[common.InventoryLabel]
}

func (ui *UnstructuredInventory) InitialInventory() Inventory {
	return ui
}

var _ Inventory = &UnstructuredInventory{}

// BaseInventory is a boilerplate struct that contains the basic methods
// to implement Inventory. Can be extended for different inventory implementations.
type BaseInventory struct {
	// Objs and ObjStatuses are in memory representations of the inventory which are
	// read and manipulated by the applier.
	Objs        object.ObjMetadataSet
	ObjStatuses []actuation.ObjectStatus
}

func (inv *BaseInventory) Objects() object.ObjMetadataSet {
	return inv.Objs
}

func (inv *BaseInventory) ObjectStatuses() []actuation.ObjectStatus {
	return inv.ObjStatuses
}

func (inv *BaseInventory) SetObjects(objs object.ObjMetadataSet) {
	inv.Objs = objs
}

func (inv *BaseInventory) SetObjectStatuses(statuses []actuation.ObjectStatus) {
	inv.ObjStatuses = statuses
}

type FromUnstructuredFunc func(*unstructured.Unstructured) (*UnstructuredInventory, error)
type ToUnstructuredFunc func(*UnstructuredInventory) (*unstructured.Unstructured, error)

// UnstructuredClient implements the inventory client interface for a single unstructured object
type UnstructuredClient struct {
	client           dynamic.NamespaceableResourceInterface
	statusPolicy     StatusPolicy
	fromUnstructured FromUnstructuredFunc
	toUnstructured   ToUnstructuredFunc
}

func NewUnstructuredClient(factory cmdutil.Factory,
	from FromUnstructuredFunc,
	to ToUnstructuredFunc,
	gvk schema.GroupVersionKind,
	statusPolicy StatusPolicy) (*UnstructuredClient, error) {
	dc, err := factory.DynamicClient()
	if err != nil {
		return nil, err
	}
	mapper, err := factory.ToRESTMapper()
	if err != nil {
		return nil, err
	}
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return nil, err
	}
	configMapClient := &UnstructuredClient{
		client:           dc.Resource(mapping.Resource),
		statusPolicy:     statusPolicy,
		fromUnstructured: from,
		toUnstructured:   to,
	}
	return configMapClient, nil
}

// Get the in-cluster inventory
func (cic *UnstructuredClient) Get(ctx context.Context, id Info, _ GetOptions) (Inventory, error) {
	if id == nil {
		return nil, fmt.Errorf("id is nil")
	}
	obj, err := cic.client.Namespace(id.Namespace()).Get(ctx, id.Name(), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return cic.fromUnstructured(obj)
}

// List the in-cluster inventory
// Used by the CLI commands
func (cic *UnstructuredClient) List(ctx context.Context, _ ListOptions) ([]Inventory, error) {
	objs, err := cic.client.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	var inventories []Inventory
	for _, obj := range objs.Items {
		uInv, err := cic.fromUnstructured(&obj)
		if err != nil {
			return nil, err
		}
		inventories = append(inventories, uInv)
	}
	return inventories, nil
}

// Update the in-cluster inventory
// Performs a simple in-place update on the ConfigMap
func (cic *UnstructuredClient) Update(ctx context.Context, inv Inventory, opts UpdateOptions) error {
	ui, ok := inv.(*UnstructuredInventory)
	if !ok {
		return fmt.Errorf("expected ConfigMapInventory")
	}
	if ui == nil {
		return fmt.Errorf("inventory is nil")
	}
	uObj, err := cic.toUnstructured(ui)
	if err != nil {
		return err
	}
	var newObj *unstructured.Unstructured
	newObj, err = cic.client.Namespace(uObj.GetNamespace()).Update(ctx, uObj, metav1.UpdateOptions{})
	if apierrors.IsNotFound(err) {
		newObj, err = cic.client.Namespace(uObj.GetNamespace()).Create(ctx, uObj, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		ui.ClusterObj = newObj
		return nil
	} else if err != nil {
		return err
	}
	ui.ClusterObj = newObj
	if cic.statusPolicy == StatusPolicyNone || !opts.UpdateStatus {
		return nil
	}
	uObj.SetResourceVersion(ui.ClusterObj.GetResourceVersion())
	_, ok, err = unstructured.NestedInt64(newObj.Object, "status", "observedGeneration")
	if err != nil {
		return err
	}
	if ok {
		err = unstructured.SetNestedField(uObj.Object, newObj.GetGeneration(), "status", "observedGeneration")
		if err != nil {
			return err
		}
	}
	newObj, err = cic.client.Namespace(uObj.GetNamespace()).UpdateStatus(ctx, uObj, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	ui.ClusterObj = newObj
	return nil
}

// Delete the in-cluster inventory
// Performs a simple deletion of the ConfigMap
func (cic *UnstructuredClient) Delete(ctx context.Context, id Info, _ DeleteOptions) error {
	if id == nil {
		return fmt.Errorf("id is nil")
	}
	if err := cic.client.Namespace(id.Namespace()).Delete(ctx, id.Name(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	return nil
}
