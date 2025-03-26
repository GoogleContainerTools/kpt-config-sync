// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"context"

	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/object"
)

const (
	// TestInventoryName is the name of the fake inventory used for unit tests
	TestInventoryName = "test-inventory"
	// TestInventoryNamespace is the namespace of the fake inventory used for unit tests
	TestInventoryNamespace = "test-namespace"
)

// FakeClient is a testing implementation of the Client interface.
type FakeClient struct {
	Inv Inventory
	Err error
}

var (
	_ Client        = &FakeClient{}
	_ ClientFactory = FakeClientFactory{}

	_ Inventory = &FakeInventory{}
)

type FakeClientFactory object.ObjMetadataSet

func (f FakeClientFactory) NewClient(cmdutil.Factory) (Client, error) {
	return NewFakeClient(object.ObjMetadataSet(f)), nil
}

// NewFakeClient returns a FakeClient.
func NewFakeClient(objs object.ObjMetadataSet) *FakeClient {
	return &FakeClient{
		Inv: &FakeInventory{
			InventoryID:        TestInventoryName,
			InventoryNamespace: TestInventoryNamespace,
			InventoryContents: InventoryContents{
				ObjectRefs: objs,
			},
		},
		Err: nil,
	}
}

// NewInventory returns a new empty inventory object
func (fic *FakeClient) NewInventory(id Info) (Inventory, error) {
	inv := &FakeInventory{
		InventoryID:        id.GetID(),
		InventoryNamespace: id.GetNamespace(),
	}
	return inv, nil
}

// Get returns currently stored inventory.
func (fic *FakeClient) Get(ctx context.Context, id Info, opts GetOptions) (Inventory, error) {
	if fic.Err != nil {
		return nil, fic.Err
	}
	return fic.Inv, nil
}

// CreateOrUpdate the stored cluster inventory objs with the passed obj, or an
// error if one is set up.
func (fic *FakeClient) CreateOrUpdate(ctx context.Context, inv Inventory, opts UpdateOptions) error {
	if fic.Err != nil {
		return fic.Err
	}
	fic.Inv = inv
	return nil
}

// Delete returns an error if one is forced; does nothing otherwise.
func (fic *FakeClient) Delete(ctx context.Context, id Info, opts DeleteOptions) error {
	if fic.Err != nil {
		return fic.Err
	}
	return nil
}

// List the in-cluster inventory
// Performs a simple in-place update on the ConfigMap
func (fic *FakeClient) List(ctx context.Context, opts ListOptions) ([]Inventory, error) {
	return nil, nil
}

// SetError forces an error on the subsequent client call if it returns an error.
func (fic *FakeClient) SetError(err error) {
	fic.Err = err
}

// ClearError clears the force error
func (fic *FakeClient) ClearError() {
	fic.Err = nil
}

type FakeInventory struct {
	InventoryContents
	InventoryID        ID
	InventoryNamespace string
}

func (fi *FakeInventory) GetID() ID {
	return fi.InventoryID
}

func (fi *FakeInventory) GetNamespace() string {
	return fi.InventoryNamespace
}

func (fi *FakeInventory) Info() Info {
	return NewSimpleInfo(fi.InventoryID, fi.InventoryNamespace)
}
