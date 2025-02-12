// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
	v1 "kpt.dev/configsync/pkg/generated/clientset/versioned/typed/configmanagement/v1"
)

type FakeConfigmanagementV1 struct {
	*testing.Fake
}

func (c *FakeConfigmanagementV1) ClusterConfigs() v1.ClusterConfigInterface {
	return newFakeClusterConfigs(c)
}

func (c *FakeConfigmanagementV1) ClusterSelectors() v1.ClusterSelectorInterface {
	return newFakeClusterSelectors(c)
}

func (c *FakeConfigmanagementV1) HierarchyConfigs() v1.HierarchyConfigInterface {
	return newFakeHierarchyConfigs(c)
}

func (c *FakeConfigmanagementV1) NamespaceConfigs() v1.NamespaceConfigInterface {
	return newFakeNamespaceConfigs(c)
}

func (c *FakeConfigmanagementV1) NamespaceSelectors() v1.NamespaceSelectorInterface {
	return newFakeNamespaceSelectors(c)
}

func (c *FakeConfigmanagementV1) Repos() v1.RepoInterface {
	return newFakeRepos(c)
}

func (c *FakeConfigmanagementV1) Syncs() v1.SyncInterface {
	return newFakeSyncs(c)
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeConfigmanagementV1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
