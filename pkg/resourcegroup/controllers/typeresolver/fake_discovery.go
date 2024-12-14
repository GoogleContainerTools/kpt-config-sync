// Copyright 2024 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package typeresolver

import (
	"fmt"

	openapi_v2 "github.com/google/gnostic-models/openapiv2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/openapi"
	restclient "k8s.io/client-go/rest"
)

// FakeDiscoveryClient implements discovery.DiscoveryInterface and fakes out the
// method calls.
type FakeDiscoveryClient struct {
	GroupsAndMaybeResourcesCalls          int
	GroupsAndMaybeResourcesOutputs        []FakeDiscoveryGroupsAndMaybeResourcesOutput
	ServerResourcesForGroupVersionCalls   int
	ServerResourcesForGroupVersionInputs  []FakeDiscoveryServerResourcesForGroupVersionInput
	ServerResourcesForGroupVersionOutputs []FakeDiscoveryServerResourcesForGroupVersionOutput
	ServerPreferredResourcesCalls         int
	ServerPreferredResourcesOutputs       []FakeDiscoveryServerPreferredResourcesOutput
}

var _ discovery.AggregatedDiscoveryInterface = &FakeDiscoveryClient{}

// FakeDiscoveryGroupsAndMaybeResourcesOutput represents the output of
// FakeDiscoveryClient.GroupsAndMaybeResources
type FakeDiscoveryGroupsAndMaybeResourcesOutput struct {
	Groups              *metav1.APIGroupList
	Resources           map[schema.GroupVersion]*metav1.APIResourceList
	FailedGroupVersions map[schema.GroupVersion]error
	Error               error
}

// FakeDiscoveryServerResourcesForGroupVersionInput represents the input to
// FakeDiscoveryClient.ServerResourcesForGroupVersion
type FakeDiscoveryServerResourcesForGroupVersionInput struct {
	GroupVersion string
}

// FakeDiscoveryServerResourcesForGroupVersionOutput represents the output of
// FakeDiscoveryClient.ServerResourcesForGroupVersion
type FakeDiscoveryServerResourcesForGroupVersionOutput struct {
	Resources *metav1.APIResourceList
	Error     error
}

// FakeDiscoveryServerPreferredResourcesOutput represents the output of
// FakeDiscoveryClient.ServerPreferredResources
type FakeDiscoveryServerPreferredResourcesOutput struct {
	Resources []*metav1.APIResourceList
	Error     error
}

// GroupsAndMaybeResources returns the API groups and their resources, if
// available, otherwise just the groups.
func (dc *FakeDiscoveryClient) GroupsAndMaybeResources() (*metav1.APIGroupList, map[schema.GroupVersion]*metav1.APIResourceList, map[schema.GroupVersion]error, error) {
	if dc.GroupsAndMaybeResourcesCalls >= len(dc.GroupsAndMaybeResourcesOutputs) {
		panic(fmt.Sprintf("Expected only %d calls to FakeDiscoveryClient.GroupsAndMaybeResources, "+
			"but got more. Update FakeDiscoveryClient.GroupsAndMaybeResourcesOutput if this is expected.",
			len(dc.GroupsAndMaybeResourcesOutputs)))
	}
	outputs := dc.GroupsAndMaybeResourcesOutputs[dc.GroupsAndMaybeResourcesCalls]
	dc.GroupsAndMaybeResourcesCalls++
	return outputs.Groups, outputs.Resources, outputs.FailedGroupVersions, outputs.Error
}

// ServerResourcesForGroupVersion returns the supported resources for a group
// and version.
func (dc *FakeDiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	dc.ServerResourcesForGroupVersionInputs = append(dc.ServerResourcesForGroupVersionInputs, FakeDiscoveryServerResourcesForGroupVersionInput{
		GroupVersion: groupVersion,
	})
	if dc.ServerResourcesForGroupVersionCalls >= len(dc.ServerResourcesForGroupVersionOutputs) {
		panic(fmt.Sprintf("Expected only %d calls to FakeDiscoveryClient.ServerResourcesForGroupVersion, "+
			"but got more. Update FakeDiscoveryClient.ServerResourcesForGroupVersionOutputs if this is expected.",
			len(dc.ServerResourcesForGroupVersionOutputs)))
	}
	outputs := dc.ServerResourcesForGroupVersionOutputs[dc.ServerResourcesForGroupVersionCalls]
	dc.ServerResourcesForGroupVersionCalls++
	return outputs.Resources, outputs.Error
}

// ServerGroupsAndResources returns the supported groups and resources for all
// groups and versions.
func (dc *FakeDiscoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	panic("unimplemented")
}

// ServerPreferredResources returns the supported resources with the version
// preferred by the server.
func (dc *FakeDiscoveryClient) ServerPreferredResources() ([]*metav1.APIResourceList, error) {
	if dc.ServerPreferredResourcesCalls >= len(dc.ServerPreferredResourcesOutputs) {
		panic(fmt.Sprintf("Expected only %d calls to FakeDiscoveryClient.ServerPreferredResources, "+
			"but got more. Update FakeDiscoveryClient.ServerPreferredResourcesOutputs if this is expected.",
			len(dc.ServerPreferredResourcesOutputs)))
	}
	outputs := dc.ServerPreferredResourcesOutputs[dc.ServerPreferredResourcesCalls]
	dc.ServerPreferredResourcesCalls++
	return outputs.Resources, outputs.Error
}

// ServerPreferredNamespacedResources returns the supported namespaced resources
// with the version preferred by the server.
func (dc *FakeDiscoveryClient) ServerPreferredNamespacedResources() ([]*metav1.APIResourceList, error) {
	panic("unimplemented")
}

// ServerGroups returns the supported groups, with information like supported
// versions and the preferred version.
func (dc *FakeDiscoveryClient) ServerGroups() (*metav1.APIGroupList, error) {
	panic("unimplemented")
}

// ServerVersion retrieves and parses the server's version.
func (dc *FakeDiscoveryClient) ServerVersion() (*version.Info, error) {
	panic("unimplemented")
}

// OpenAPISchema retrieves and parses the swagger API schema the server supports.
func (dc *FakeDiscoveryClient) OpenAPISchema() (*openapi_v2.Document, error) {
	panic("unimplemented")
}

// OpenAPIV3 returns the OpenAPI v3 client.
func (dc *FakeDiscoveryClient) OpenAPIV3() openapi.Client {
	panic("unimplemented")
}

// RESTClient returns a RESTClient that is used to communicate with API server
// by this client implementation.
func (dc *FakeDiscoveryClient) RESTClient() restclient.Interface {
	panic("unimplemented")
}

// WithLegacy returns a copy of current discovery client that will only receive
// the legacy discovery format, or pointer to current discovery client if it
// does not support legacy-only discovery.
func (dc *FakeDiscoveryClient) WithLegacy() discovery.DiscoveryInterface {
	panic("unimplemented")
}
