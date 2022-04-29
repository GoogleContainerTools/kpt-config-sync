// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apis/actuation"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// FakeClient is a testing implementation of the Client interface.
type FakeClient struct {
	Objs   object.ObjMetadataSet
	Status []actuation.ObjectStatus
	Err    error
}

var (
	_ Client        = &FakeClient{}
	_ ClientFactory = FakeClientFactory{}
)

type FakeClientFactory object.ObjMetadataSet

func (f FakeClientFactory) NewClient(cmdutil.Factory) (Client, error) {
	return NewFakeClient(object.ObjMetadataSet(f)), nil
}

// NewFakeClient returns a FakeClient.
func NewFakeClient(initObjs object.ObjMetadataSet) *FakeClient {
	return &FakeClient{
		Objs: initObjs,
		Err:  nil,
	}
}

// GetClusterObjs returns currently stored set of objects.
func (fic *FakeClient) GetClusterObjs(Info) (object.ObjMetadataSet, error) {
	if fic.Err != nil {
		return object.ObjMetadataSet{}, fic.Err
	}
	return fic.Objs, nil
}

// Merge stores the passed objects with the current stored cluster inventory
// objects. Returns the set difference of the current set of objects minus
// the passed set of objects, or an error if one is set up.
func (fic *FakeClient) Merge(_ Info, objs object.ObjMetadataSet, _ common.DryRunStrategy) (object.ObjMetadataSet, error) {
	if fic.Err != nil {
		return object.ObjMetadataSet{}, fic.Err
	}
	diffObjs := fic.Objs.Diff(objs)
	fic.Objs = fic.Objs.Union(objs)
	return diffObjs, nil
}

// Replace the stored cluster inventory objs with the passed obj, or an
// error if one is set up.
func (fic *FakeClient) Replace(_ Info, objs object.ObjMetadataSet, status []actuation.ObjectStatus,
	_ common.DryRunStrategy) error {
	if fic.Err != nil {
		return fic.Err
	}
	fic.Objs = objs
	fic.Status = status
	return nil
}

// DeleteInventoryObj returns an error if one is forced; does nothing otherwise.
func (fic *FakeClient) DeleteInventoryObj(Info, common.DryRunStrategy) error {
	if fic.Err != nil {
		return fic.Err
	}
	return nil
}

func (fic *FakeClient) ApplyInventoryNamespace(*unstructured.Unstructured, common.DryRunStrategy) error {
	if fic.Err != nil {
		return fic.Err
	}
	return nil
}

// SetError forces an error on the subsequent client call if it returns an error.
func (fic *FakeClient) SetError(err error) {
	fic.Err = err
}

// ClearError clears the force error
func (fic *FakeClient) ClearError() {
	fic.Err = nil
}

func (fic *FakeClient) GetClusterInventoryInfo(Info) (*unstructured.Unstructured, error) {
	return nil, nil
}

func (fic *FakeClient) GetClusterInventoryObjs(_ Info) (object.UnstructuredSet, error) {
	return object.UnstructuredSet{}, nil
}
