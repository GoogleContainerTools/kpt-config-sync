// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package fake

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ClusterReader struct {
	NoopClusterReader

	GetResource *unstructured.Unstructured
	GetErr      error

	ListResources *unstructured.UnstructuredList
	ListErr       error

	SyncErr error
}

func (f *ClusterReader) Get(_ context.Context, _ client.ObjectKey, u *unstructured.Unstructured) error {
	if f.GetResource != nil {
		u.Object = f.GetResource.Object
	}
	return f.GetErr
}

func (f *ClusterReader) ListNamespaceScoped(_ context.Context, list *unstructured.UnstructuredList, _ string, _ labels.Selector) error {
	if f.ListResources != nil {
		list.Items = f.ListResources.Items
	}
	return f.ListErr
}

func (f *ClusterReader) Sync(_ context.Context) error {
	return f.SyncErr
}

func NewNoopClusterReader() *NoopClusterReader {
	return &NoopClusterReader{}
}

type NoopClusterReader struct{}

func (n *NoopClusterReader) Get(_ context.Context, _ client.ObjectKey, _ *unstructured.Unstructured) error {
	return nil
}

func (n *NoopClusterReader) ListNamespaceScoped(_ context.Context, _ *unstructured.UnstructuredList,
	_ string, _ labels.Selector) error {
	return nil
}

func (n *NoopClusterReader) ListClusterScoped(_ context.Context, _ *unstructured.UnstructuredList,
	_ labels.Selector) error {
	return nil
}

func (n *NoopClusterReader) Sync(_ context.Context) error {
	return nil
}
