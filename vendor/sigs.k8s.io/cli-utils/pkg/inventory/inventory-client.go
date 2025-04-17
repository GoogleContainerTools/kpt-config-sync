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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Client expresses an interface for interacting with
// objects which store references to objects (inventory objects).
type Client interface {
	ReadClient
	WriteClient
	Factory
}

// ID is a unique identifier for an inventory object.
// It's used by the inventory client to get/update/delete the object.
// For example, it could be a name or a label depending on the inventory client
// implementation.
type ID string

// String implements the fmt.Stringer interface for ID
func (id ID) String() string {
	return string(id)
}

// Info provides the minimal information for the applier
// to create, look up and update an inventory.
// The inventory object can be any type, the Provider in the applier
// needs to know how to create, look up and update it based
// on the Info.
type Info interface {
	// GetNamespace of the inventory object.
	// It should be the value of the field .metadata.namespace.
	GetNamespace() string

	// GetID of the inventory object.
	// The inventory client uses this to determine how to get/update the object(s)
	// from the cluster.
	GetID() ID
}

// SimpleInfo is the simplest implementation of the Info interface
type SimpleInfo struct {
	// id of the inventory object
	id ID
	// namespace of the inventory object
	namespace string
}

// GetID returns the ID of the inventory object
func (i *SimpleInfo) GetID() ID {
	return i.id
}

// GetNamespace returns the namespace of the inventory object
func (i *SimpleInfo) GetNamespace() string {
	return i.namespace
}

// NewSimpleInfo constructs a new SimpleInfo
func NewSimpleInfo(id ID, namespace string) *SimpleInfo {
	return &SimpleInfo{
		id:        id,
		namespace: namespace,
	}
}

// SingleObjectInfo implements the Info interface and also adds a Name field.
// This assumes the underlying inventory is a singleton object with a single
// name. Used by SingleObjectInventory, which assumes a single object.
type SingleObjectInfo struct {
	SimpleInfo
	// name of the singleton inventory object
	name string
}

// GetName returns the name of the inventory object
func (i *SingleObjectInfo) GetName() string {
	return i.name
}

// NewSingleObjectInfo constructs a new SingleObjectInfo
func NewSingleObjectInfo(id ID, nn types.NamespacedName) *SingleObjectInfo {
	return &SingleObjectInfo{
		SimpleInfo: SimpleInfo{
			id:        id,
			namespace: nn.Namespace,
		},
		name: nn.Name,
	}
}

type Factory interface {
	// NewInventory returns an empty initialized inventory object.
	// This is used in the case that there is no existing object on the cluster.
	NewInventory(Info) (Inventory, error)
}

// Inventory is an internal representation of the inventory object that is used
// to track which objects have been applied by the applier. The interface exists
// so different underlying resource types can be used to implement the inventory.
type Inventory interface {
	Info() Info
	// GetObjectRefs returns the list of object references tracked in the inventory
	GetObjectRefs() object.ObjMetadataSet
	// GetObjectStatuses returns the list of statuses for each object reference tracked in the inventory
	GetObjectStatuses() object.ObjectStatusSet
	// SetObjectRefs updates the local cache of object references tracked in the inventory.
	// This will be persisted to the cluster when the Inventory is passed to CreateOrUpdate.
	SetObjectRefs(object.ObjMetadataSet)
	// SetObjectStatuses updates the local cache of object statuses tracked in the inventory.
	// This will be persisted to the cluster when the Inventory is passed to CreateOrUpdate.
	SetObjectStatuses(object.ObjectStatusSet)
}

type ReadClient interface {
	// Get an inventory object from the cluster.
	Get(ctx context.Context, inv Info, opts GetOptions) (Inventory, error)
	// List the inventory objects from the cluster. This is used by the CLI and
	// not used by the applier/destroyer.
	List(ctx context.Context, opts ListOptions) ([]Inventory, error)
}

type WriteClient interface {
	// CreateOrUpdate an inventory object on the cluster. Always updates spec,
	// and will update the status if StatusPolicyAll is used.
	CreateOrUpdate(ctx context.Context, inv Inventory, opts UpdateOptions) error
	// Delete an inventory object on the cluster.
	Delete(ctx context.Context, inv Info, opts DeleteOptions) error
}

// UpdateOptions are used to configure the behavior of the CreateOrUpdate method.
// These options are set by the applier and should be honored by the inventory
// client implementation.
type UpdateOptions struct{}

// GetOptions are used to configure the behavior of the Get method.
// These options are set by the applier and should be honored by the inventory
// client implementation.
type GetOptions struct{}

// ListOptions are used to configure the behavior of the List method.
// These options are set by the applier and should be honored by the inventory
// client implementation.
type ListOptions struct{}

// DeleteOptions are used to configure the behavior of the Delete method.
// These options are set by the applier and should be honored by the inventory
// client implementation.
type DeleteOptions struct{}

var _ Client = &UnstructuredClient{}

// NewSingleObjectInventory constructs a new SingleObjectInventory object
// from a raw unstructured object.
func NewSingleObjectInventory(uObj *unstructured.Unstructured) *SingleObjectInventory {
	return &SingleObjectInventory{
		SingleObjectInfo: SingleObjectInfo{
			SimpleInfo: SimpleInfo{
				id:        ID(uObj.GetLabels()[common.InventoryLabel]),
				namespace: uObj.GetNamespace(),
			},
			name: uObj.GetName(),
		},
	}
}

// SingleObjectInventory implements Inventory while also retaining a reference
// to a singleton KRM object from the cluster. This enables the client to update
// set the object metadata for create/update/delete calls.
type SingleObjectInventory struct {
	SingleObjectInfo
	InventoryContents
}

// Info returns the metadata associated with this SingleObjectInventory
func (ui *SingleObjectInventory) Info() Info {
	return &ui.SingleObjectInfo
}

var _ Inventory = &SingleObjectInventory{}

// InventoryContents is a boilerplate struct that contains the basic methods
// to implement Inventory. Can be extended for different inventory implementations.
// nolint:revive
type InventoryContents struct {
	// ObjectRefs and ObjectStatuses are in memory representations of the inventory which are
	// read and manipulated by the applier.
	ObjectRefs     object.ObjMetadataSet
	ObjectStatuses object.ObjectStatusSet
}

// GetObjectRefs returns the list of object references tracked in the inventory
func (inv *InventoryContents) GetObjectRefs() object.ObjMetadataSet {
	return inv.ObjectRefs
}

// GetObjectStatuses returns the list of statuses for each object reference tracked in the inventory
func (inv *InventoryContents) GetObjectStatuses() object.ObjectStatusSet {
	return inv.ObjectStatuses
}

// SetObjectRefs updates the local cache of object references tracked in the inventory.
// This will be persisted to the cluster when the Inventory is passed to CreateOrUpdate.
func (inv *InventoryContents) SetObjectRefs(objs object.ObjMetadataSet) {
	inv.ObjectRefs = objs
}

// SetObjectStatuses updates the local cache of object statuses tracked in the inventory.
// This will be persisted to the cluster when the Inventory is passed to CreateOrUpdate.
func (inv *InventoryContents) SetObjectStatuses(statuses object.ObjectStatusSet) {
	inv.ObjectStatuses = statuses
}

// FromUnstructuredFunc is used by UnstructuredClient to convert an unstructured
// object, usually fetched from the cluster, to an Inventory object for use by
// the applier/destroyer.
type FromUnstructuredFunc func(fromObj *unstructured.Unstructured) (*SingleObjectInventory, error)

// ToUnstructuredFunc is used by UnstructuredClient to take an unstructured object,
// usually fetched from the cluster, and update it using the SingleObjectInventory.
// The function should return an updated unstructured object which will then be
// applied to the cluster by UnstructuredClient.
type ToUnstructuredFunc func(fromObj *unstructured.Unstructured, toInv *SingleObjectInventory) (*unstructured.Unstructured, error)

// UnstructuredClient implements the inventory client interface for a single unstructured object
type UnstructuredClient struct {
	// client is the namespaced dynamic client to use when reading or writing
	// the inventory objects to and from the server.
	client dynamic.NamespaceableResourceInterface
	// fromUnstructured is used in NewInventory, Get, and List, to convert the
	// object(s) from Unstructured to UnstructuredInventory.
	fromUnstructured FromUnstructuredFunc
	// toUnstructured is used in CreateOrUpdate, to convert the object(s) from
	// UnstructuredInventory to Unstructured. This is called before calling
	// Create or Update.
	toUnstructured ToUnstructuredFunc
	// toUnstructuredStatus is used in CreateOrUpdate, to convert the object(s)
	// from UnstructuredInventory to Unstructured. This is called before calling
	// UpdateStatus. Set to nil to skip calling UpdateStatus, either because
	// there is no status or because the status is not a subresource.
	toUnstructuredStatus ToUnstructuredFunc
	// gvk is the GroupVersionKind of the inventory object in the Kubernetes API.
	gvk schema.GroupVersionKind
}

// NewUnstructuredClient constructs an instance of UnstructuredClient.
// This can be used as an inventory client for any arbitrary GVK, assuming it
// is a singleton object.
func NewUnstructuredClient(factory cmdutil.Factory,
	from FromUnstructuredFunc,
	to ToUnstructuredFunc,
	toStatus ToUnstructuredFunc,
	gvk schema.GroupVersionKind,
) (*UnstructuredClient, error) {
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
		client:               dc.Resource(mapping.Resource),
		fromUnstructured:     from,
		toUnstructured:       to,
		toUnstructuredStatus: toStatus,
		gvk:                  gvk,
	}
	return unstructuredClient, nil
}

// NewInventory returns a new, empty Inventory object
func (cic *UnstructuredClient) NewInventory(inv Info) (Inventory, error) {
	soi, ok := inv.(*SingleObjectInfo)
	if !ok {
		return nil, fmt.Errorf("expected SingleObjectInfo but got %T", inv)
	}
	return cic.fromUnstructured(cic.newUnstructuredObject(soi))
}

// newInventory is used internally to return a typed SingleObjectInventory
func (cic *UnstructuredClient) newUnstructuredObject(invInfo *SingleObjectInfo) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetName(invInfo.GetName())
	obj.SetNamespace(invInfo.GetNamespace())
	obj.SetGroupVersionKind(cic.gvk)
	obj.SetLabels(map[string]string{common.InventoryLabel: invInfo.GetID().String()})
	return obj
}

// Get the in-cluster inventory
func (cic *UnstructuredClient) Get(ctx context.Context, inv Info, _ GetOptions) (Inventory, error) {
	if inv == nil {
		return nil, fmt.Errorf("inventory Info is nil")
	}
	soi, ok := inv.(*SingleObjectInfo)
	if !ok {
		return nil, fmt.Errorf("expected SingleObjectInfo but got %T", inv)
	}
	obj, err := cic.client.Namespace(soi.GetNamespace()).Get(ctx, soi.GetName(), metav1.GetOptions{})
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
func (cic *UnstructuredClient) CreateOrUpdate(ctx context.Context, inv Inventory, opts UpdateOptions) error {
	ui, ok := inv.(*SingleObjectInventory)
	if !ok {
		return fmt.Errorf("expected SingleObjectInventory")
	}
	if ui == nil {
		return fmt.Errorf("inventory is nil")
	}
	// Attempt to retry on a resource conflict error to avoid needing to retry the
	// entire Apply/Destroy when there's a transient conflict.
	attempt := 0
	var obj *unstructured.Unstructured
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		defer func() {
			attempt++
		}()
		create := false
		var err error
		obj, err = cic.client.Namespace(ui.GetNamespace()).Get(ctx, ui.GetName(), metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			create = true
			obj = cic.newUnstructuredObject(&ui.SingleObjectInfo)
		} else if err != nil {
			return err
		}
		uObj, err := cic.toUnstructured(obj.DeepCopy(), ui)
		if err != nil {
			return err
		}
		if create {
			klog.V(4).Infof("[attempt %d] creating inventory object %s/%s/%s",
				attempt, cic.gvk, uObj.GetNamespace(), uObj.GetName())
			obj, err = cic.client.Namespace(uObj.GetNamespace()).Create(ctx, uObj, metav1.CreateOptions{})
			if err != nil {
				return err
			}
		} else {
			klog.V(4).Infof("[attempt %d] updating inventory object %s/%s/%s",
				attempt, cic.gvk, uObj.GetNamespace(), uObj.GetName())
			obj, err = cic.client.Namespace(uObj.GetNamespace()).Update(ctx, uObj, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// toUnstructuredStatus being nil means that there is no status subresource
	if cic.toUnstructuredStatus == nil {
		return nil
	}
	attempt = 0
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		defer func() {
			attempt++
		}()
		if attempt > 0 { // reuse object from spec update/create on first attempt
			obj, err = cic.client.Namespace(ui.GetNamespace()).Get(ctx, ui.GetName(), metav1.GetOptions{})
			if err != nil {
				return err
			}
		}
		uObj, err := cic.toUnstructuredStatus(obj.DeepCopy(), ui)
		if err != nil {
			return err
		}
		// Update observedGeneration, if it exists
		// If using a custom inventory resource, make sure the observedGeneration
		// defaults to 0. That will ensure the observedGeneration is updated here, and
		// the kstatus computes as InProgress after the object is created.
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
		klog.V(4).Infof("[attempt %d] updating status of inventory object %s/%s/%s",
			attempt, cic.gvk, uObj.GetNamespace(), uObj.GetName())
		_, err = cic.client.Namespace(uObj.GetNamespace()).UpdateStatus(ctx, uObj, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("updateStatus: %w", err)
		}
		return nil
	})
}

// Delete the in-cluster inventory
// Performs a simple deletion of the unstructured object
func (cic *UnstructuredClient) Delete(ctx context.Context, inv Info, _ DeleteOptions) error {
	if inv == nil {
		return fmt.Errorf("inventory Info is nil")
	}
	soi, ok := inv.(*SingleObjectInfo)
	if !ok {
		return fmt.Errorf("expected SingleObjectInfo but got %T", inv)
	}
	err := cic.client.Namespace(soi.GetNamespace()).Delete(ctx, soi.GetName(), metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete: %w", err)
	}
	return nil
}
