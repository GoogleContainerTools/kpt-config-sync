// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeConfigsyncV1beta1 struct {
	*testing.Fake
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeConfigsyncV1beta1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
