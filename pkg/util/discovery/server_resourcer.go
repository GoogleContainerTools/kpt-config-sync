// Copyright 2022 Google LLC
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

package discovery

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/status"
)

// ServerResourcer returns a the API Groups and API Resources available on the
// API Server.
//
// DiscoveryInterface satisfies this interface.
type ServerResourcer interface {
	ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error)
}

// NoOpServerResourcer is a ServerResourcer that returns nil.
type NoOpServerResourcer struct{}

// ServerGroupsAndResources returns nothing.
func (n NoOpServerResourcer) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, nil, nil
}

var _ ServerResourcer = NoOpServerResourcer{}

type invalidatable interface {
	Invalidate()
}

// GetResources gets the APIResourceLists from an existing DiscoveryClient.
// Invalidates the cache if possible as the server may have new resources since the client was created.
func GetResources(discoveryClient ServerResourcer) ([]*metav1.APIResourceList, status.MultiError) {
	if invalidatableDiscoveryClient, isInvalidatable := discoveryClient.(invalidatable); isInvalidatable {
		// Non-cached DiscoveryClients aren't invalidatable, so we have to allow for this possibility.
		invalidatableDiscoveryClient.Invalidate()
	}
	_, resourceLists, discoveryErr := discoveryClient.ServerGroupsAndResources()
	if discoveryErr != nil {
		// Apparently the ServerResources batches a bunch of discovery requests calls
		// and the author decided that it's perfectly reasonable to return an error
		// for failure on any of those calls (despite some succeeding), so we
		// check for this specific error then ignore it while logging a warning.
		// It's not clear how we should handle this error since there's not a good
		// way to determine if we really needed the discovery info from that one
		// group that failed and something is going horribly wrong, or if someone
		// decided to have fun with adding broken APIServices.  In any case, this is
		// Kubernetes so we are going to continue onward in the name of eventual
		// consistency, tally-ho!
		if discovery.IsGroupDiscoveryFailedError(discoveryErr) {
			klog.Warningf("failed to discover some APIGroups: %s", discoveryErr)
		} else {
			return nil, status.APIServerError(discoveryErr, "failed to get server resources")
		}
	}
	return resourceLists, nil
}

// APIResourceScoper returns a Scoper that contains scopes for all resources
// available from the API server.
func APIResourceScoper(sr ServerResourcer) (Scoper, status.MultiError) {
	scoper := Scoper{}

	// List the APIResources from the API Server and add them.
	lists, discoveryErr := GetResources(sr)
	if discoveryErr != nil {
		return scoper, discoveryErr
	}

	addListsErr := scoper.AddAPIResourceLists(lists)
	return scoper, addListsErr
}
