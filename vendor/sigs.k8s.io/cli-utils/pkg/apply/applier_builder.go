// Copyright 2021 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package apply

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apply/info"
	"sigs.k8s.io/cli-utils/pkg/apply/prune"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/kstatus/watcher"
)

type ApplierBuilder struct {
	commonBuilder
}

// NewApplierBuilder returns a new ApplierBuilder.
func NewApplierBuilder() *ApplierBuilder {
	return &ApplierBuilder{
		// Defaults, if any, go here.
	}
}

func (b *ApplierBuilder) Build() (*Applier, error) {
	bx, err := b.finalize()
	if err != nil {
		return nil, err
	}
	return &Applier{
		pruner: &prune.Pruner{
			InvClient: bx.invClient,
			Client:    bx.client,
			Mapper:    bx.mapper,
		},
		statusWatcher: bx.statusWatcher,
		invClient:     bx.invClient,
		client:        bx.client,
		openAPIGetter: bx.discoClient,
		mapper:        bx.mapper,
		infoHelper:    info.NewHelper(bx.mapper, bx.unstructuredClientForMapping),
	}, nil
}

func (b *ApplierBuilder) WithFactory(factory util.Factory) *ApplierBuilder {
	b.factory = factory
	return b
}

func (b *ApplierBuilder) WithInventoryClient(invClient inventory.Client) *ApplierBuilder {
	b.invClient = invClient
	return b
}

func (b *ApplierBuilder) WithDynamicClient(client dynamic.Interface) *ApplierBuilder {
	b.client = client
	return b
}

func (b *ApplierBuilder) WithDiscoveryClient(discoClient discovery.CachedDiscoveryInterface) *ApplierBuilder {
	b.discoClient = discoClient
	return b
}

func (b *ApplierBuilder) WithRestMapper(mapper meta.RESTMapper) *ApplierBuilder {
	b.mapper = mapper
	return b
}

func (b *ApplierBuilder) WithRestConfig(restConfig *rest.Config) *ApplierBuilder {
	b.restConfig = restConfig
	return b
}

func (b *ApplierBuilder) WithUnstructuredClientForMapping(unstructuredClientForMapping func(*meta.RESTMapping) (resource.RESTClient, error)) *ApplierBuilder {
	b.unstructuredClientForMapping = unstructuredClientForMapping
	return b
}

func (b *ApplierBuilder) WithStatusWatcher(statusWatcher watcher.StatusWatcher) *ApplierBuilder {
	b.statusWatcher = statusWatcher
	return b
}
