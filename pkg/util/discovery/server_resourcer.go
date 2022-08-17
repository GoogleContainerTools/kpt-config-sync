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
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
		// b/238836947 ServerGroupsAndResources still returns the resources it discovered when there was an error.
		// Most errors are fatal, but we want to ignore NotFound errors. This allows for CRDs and CRs to be applied
		// with a two-pass apply & retry strategy, where objects with a NotFound resources are skipped until the
		// resource is registered. Other errors must cause failure, otherwise they might cause existing objects to be pruned.
		var discoErr *discovery.ErrGroupDiscoveryFailed
		if errors.As(discoveryErr, &discoErr) {
			for gv, subErr := range discoErr.Groups {
				if apierrors.IsNotFound(subErr) {
					klog.Warningf("Failed to discover APIGroups %s due to not found, moving on: %v", gv, subErr)
				} else {
					// return all the discovery errors if any one of them is a blocking error
					return nil, status.APIServerError(discoveryErr, "API discovery failed")
				}
			}
		} else {
			return nil, status.APIServerError(discoveryErr, "API discovery failed")
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
