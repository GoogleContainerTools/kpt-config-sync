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
	"k8s.io/client-go/util/retry"
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

// ID is a unique identifier for an inventory object
type ID string

// Info provides the minimal information for the applier
// to create, look up and update an inventory.
// The inventory object can be any type, the Provider in the applier
// needs to know how to create, look up and update it based
// on the Info.
type Info interface {
	// Namespace of the inventory object.
	// It should be the value of the field .metadata.namespace.
	Namespace() string

	// ID of the inventory object.
	// The inventory client uses this to determine how to get/update the object(s)
	// from the cluster.
	ID() ID

	// NewEmptyInventory returns an empty initialized inventory object.
	// This is used in the case that there is no existing object on the cluster.
	NewEmptyInventory() Inventory
}

type Inventory interface {
	Info
	Objects() object.ObjMetadataSet
	ObjectStatuses() []actuation.ObjectStatus
	SetObjects(object.ObjMetadataSet)
	SetObjectStatuses([]actuation.ObjectStatus)
}

type ReadClient interface {
	Get(ctx context.Context, inv Info, opts GetOptions) (Inventory, error)
	List(ctx context.Context, opts ListOptions) ([]Inventory, error)
}

type WriteClient interface {
	CreateOrUpdate(ctx context.Context, inv Inventory, opts UpdateOptions) error
	UpdateStatus(ctx context.Context, inv Inventory, opts UpdateOptions) error
	Delete(ctx context.Context, inv Info, opts DeleteOptions) error
}

type UpdateOptions struct{}

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

func (ui *UnstructuredInventory) ID() ID {
	if ui.ClusterObj == nil {
		return ""
	}
	// Empty string if not set.
	return ID(ui.ClusterObj.GetLabels()[common.InventoryLabel])
}

func (ui *UnstructuredInventory) NewEmptyInventory() Inventory {
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
	fromUnstructured FromUnstructuredFunc
	toUnstructured   ToUnstructuredFunc
}

func NewUnstructuredClient(factory cmdutil.Factory,
	from FromUnstructuredFunc,
	to ToUnstructuredFunc,
	gvk schema.GroupVersionKind) (*UnstructuredClient, error) {
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
	unstructuredClient := &UnstructuredClient{
		client:           dc.Resource(mapping.Resource),
		fromUnstructured: from,
		toUnstructured:   to,
	}
	return unstructuredClient, nil
}

// Get the in-cluster inventory
func (cic *UnstructuredClient) Get(ctx context.Context, inv Info, _ GetOptions) (Inventory, error) {
	ui, ok := inv.(*UnstructuredInventory)
	if !ok {
		return nil, fmt.Errorf("expected UnstructuredInventory")
	}
	if ui == nil {
		return nil, fmt.Errorf("id is nil")
	}
	obj, err := cic.client.Namespace(ui.Namespace()).Get(ctx, ui.Name(), metav1.GetOptions{})
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

// CreateOrUpdate the in-cluster inventory
// Updates the unstructured object, or creates it if it doesn't exist
func (cic *UnstructuredClient) CreateOrUpdate(ctx context.Context, inv Inventory, _ UpdateOptions) error {
	ui, ok := inv.(*UnstructuredInventory)
	if !ok {
		return fmt.Errorf("expected UnstructuredInventory")
	}
	if ui == nil {
		return fmt.Errorf("inventory is nil")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		create := false
		obj, err := cic.client.Namespace(ui.Namespace()).Get(ctx, ui.Name(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			create = true
		} else if err != nil {
			return err
		}
		if obj != nil { // Obj is nil when IsNotFound, in this case keep initial/empty obj
			ui.ClusterObj = obj
		}
		uObj, err := cic.toUnstructured(ui)
		if err != nil {
			return err
		}
		var newObj *unstructured.Unstructured
		if create {
			newObj, err = cic.client.Namespace(uObj.GetNamespace()).Create(ctx, uObj, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			newObj, err = cic.client.Namespace(uObj.GetNamespace()).Update(ctx, uObj, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
		ui.ClusterObj = newObj
		return nil
	})
}

// UpdateStatus updates the status of the in-cluster inventory
// Performs a simple in-place update on the unstructured object
func (cic *UnstructuredClient) UpdateStatus(ctx context.Context, inv Inventory, _ UpdateOptions) error {
	ui, ok := inv.(*UnstructuredInventory)
	if !ok {
		return fmt.Errorf("expected UnstructuredInventory")
	}
	if ui == nil {
		return fmt.Errorf("inventory is nil")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		obj, err := cic.client.Namespace(ui.Namespace()).Get(ctx, ui.Name(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		ui.ClusterObj = obj
		uObj, err := cic.toUnstructured(ui)
		if err != nil {
			return err
		}
		// Update observedGeneration, if it exists
		_, ok, err = unstructured.NestedInt64(uObj.Object, "status", "observedGeneration")
		if err != nil {
			return err
		}
		if ok {
			err = unstructured.SetNestedField(uObj.Object, uObj.GetGeneration(), "status", "observedGeneration")
			if err != nil {
				return err
			}
		}
		newObj, err := cic.client.Namespace(uObj.GetNamespace()).UpdateStatus(ctx, uObj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updateStatus: %w", err)
		}
		ui.ClusterObj = newObj
		return nil
	})
}

// Delete the in-cluster inventory
// Performs a simple deletion of the unstructured object
func (cic *UnstructuredClient) Delete(ctx context.Context, inv Info, _ DeleteOptions) error {
	ui, ok := inv.(*UnstructuredInventory)
	if !ok {
		return fmt.Errorf("expected UnstructuredInventory")
	}
	if ui == nil {
		return fmt.Errorf("id is nil")
	}
	if err := cic.client.Namespace(ui.Namespace()).Delete(ctx, ui.Name(), metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
