// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package inventory

import cmdutil "k8s.io/kubectl/pkg/cmd/util"

var (
	_ ClientFactory = ClusterClientFactory{}
)

// ClientFactory is a factory that constructs new Client instances.
type ClientFactory interface {
	NewClient(factory cmdutil.Factory) (Client, error)
}

// ClusterClientFactory is a factory that creates instances of ClusterClient inventory client.
type ClusterClientFactory struct {
	StatusPolicy StatusPolicy
}

func (ccf ClusterClientFactory) NewClient(factory cmdutil.Factory) (Client, error) {
	return NewClient(factory, WrapInventoryObj, InvInfoToConfigMap, ccf.StatusPolicy)
}
