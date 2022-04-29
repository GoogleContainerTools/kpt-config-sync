// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package manifestreader

import (
	"io"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

type FakeLoader struct {
	Factory   util.Factory
	InvClient *inventory.FakeClient
}

var _ ManifestLoader = &FakeLoader{}

func NewFakeLoader(f util.Factory, objs object.ObjMetadataSet) *FakeLoader {
	return &FakeLoader{
		Factory:   f,
		InvClient: inventory.NewFakeClient(objs),
	}
}

func (f *FakeLoader) ManifestReader(reader io.Reader, _ string) (ManifestReader, error) {
	mapper, err := f.Factory.ToRESTMapper()
	if err != nil {
		return nil, err
	}

	readerOptions := ReaderOptions{
		Mapper:    mapper,
		Namespace: metav1.NamespaceDefault,
	}
	return &StreamManifestReader{
		ReaderName:    "stdin",
		Reader:        reader,
		ReaderOptions: readerOptions,
	}, nil
}

func (f *FakeLoader) InventoryInfo(objs []*unstructured.Unstructured) (inventory.Info, []*unstructured.Unstructured, error) {
	inv, objs, err := inventory.SplitUnstructureds(objs)
	return inventory.WrapInventoryInfoObj(inv), objs, err
}
