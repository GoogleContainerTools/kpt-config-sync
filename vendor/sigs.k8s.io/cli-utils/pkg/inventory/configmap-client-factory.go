// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import (
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

var (
	_ ClientFactory = ConfigMapClientFactory{}
)

// ClientFactory is a factory that constructs new Client instances.
type ClientFactory interface {
	NewClient(factory cmdutil.Factory) (Client, error)
}

// ConfigMapClientFactory is a factory that creates instances of inventory clients
// which are backed by ConfigMaps.
type ConfigMapClientFactory struct {
	StatusEnabled bool
}

func (ccf ConfigMapClientFactory) NewClient(factory cmdutil.Factory) (Client, error) {
	return NewUnstructuredClient(factory, configMapToInventory(ccf.StatusEnabled), inventoryToConfigMap(ccf.StatusEnabled), nil, ConfigMapGVK)
}
