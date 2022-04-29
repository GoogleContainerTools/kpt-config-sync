// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package info

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/cli-runtime/pkg/resource"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// Helper provides functions for interacting with Info objects.
type Helper interface {
	// UpdateInfo sets the mapping and client for the provided Info
	// object. This must be called at a time when all needed resource
	// types are available in the RESTMapper.
	UpdateInfo(info *resource.Info) error

	BuildInfo(obj *unstructured.Unstructured) (*resource.Info, error)
}

func NewHelper(mapper meta.RESTMapper, unstructuredClientForMapping func(*meta.RESTMapping) (resource.RESTClient, error)) Helper {
	return &helper{
		mapper:                       mapper,
		unstructuredClientForMapping: unstructuredClientForMapping,
	}
}

type helper struct {
	mapper                       meta.RESTMapper
	unstructuredClientForMapping func(*meta.RESTMapping) (resource.RESTClient, error)
}

func (ih *helper) UpdateInfo(info *resource.Info) error {
	gvk := info.Object.GetObjectKind().GroupVersionKind()
	mapping, err := ih.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return err
	}
	info.Mapping = mapping

	c, err := ih.unstructuredClientForMapping(mapping)
	if err != nil {
		return err
	}
	info.Client = c
	return nil
}

func (ih *helper) BuildInfo(obj *unstructured.Unstructured) (*resource.Info, error) {
	info, err := object.UnstructuredToInfo(obj)
	if err != nil {
		return nil, err
	}
	err = ih.UpdateInfo(info)
	return info, err
}
