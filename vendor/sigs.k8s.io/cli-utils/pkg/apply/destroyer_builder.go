// Copyright 2022 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package apply

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apply/info"
	"sigs.k8s.io/cli-utils/pkg/apply/prune"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/kstatus/watcher"
)

type DestroyerBuilder struct {
	commonBuilder
}

// NewDestroyerBuilder returns a new DestroyerBuilder.
func NewDestroyerBuilder() *DestroyerBuilder {
	return &DestroyerBuilder{
		// Defaults, if any, go here.
	}
}

func (b *DestroyerBuilder) Build() (*Destroyer, error) {
	bx, err := b.finalize()
	if err != nil {
		return nil, err
	}
	return &Destroyer{
		pruner: &prune.Pruner{
			InvClient: bx.invClient,
			Client:    bx.client,
			Mapper:    bx.mapper,
		},
		statusWatcher: bx.statusWatcher,
		invClient:     bx.invClient,
		mapper:        bx.mapper,
		client:        bx.client,
		infoHelper:    info.NewHelper(bx.mapper, bx.unstructuredClientForMapping),
	}, nil
}

func (b *DestroyerBuilder) WithFactory(factory util.Factory) *DestroyerBuilder {
	b.factory = factory
	return b
}

func (b *DestroyerBuilder) WithInventoryClient(invClient inventory.Client) *DestroyerBuilder {
	b.invClient = invClient
	return b
}

func (b *DestroyerBuilder) WithDynamicClient(client dynamic.Interface) *DestroyerBuilder {
	b.client = client
	return b
}

func (b *DestroyerBuilder) WithRestMapper(mapper meta.RESTMapper) *DestroyerBuilder {
	b.mapper = mapper
	return b
}

func (b *DestroyerBuilder) WithRestConfig(restConfig *rest.Config) *DestroyerBuilder {
	b.restConfig = restConfig
	return b
}

func (b *DestroyerBuilder) WithUnstructuredClientForMapping(unstructuredClientForMapping func(*meta.RESTMapping) (resource.RESTClient, error)) *DestroyerBuilder {
	b.unstructuredClientForMapping = unstructuredClientForMapping
	return b
}

func (b *DestroyerBuilder) WithStatusWatcher(statusWatcher watcher.StatusWatcher) *DestroyerBuilder {
	b.statusWatcher = statusWatcher
	return b
}
