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

package fake

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/util/discovery"
)

// discoveryClient implements the subset of the DiscoveryInterface used by the
// Syncer.
type discoveryClient struct {
	resources []*metav1.APIResourceList
}

// ServerResources implements discovery.ServerResourcer.
func (d discoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return nil, d.resources, nil
}

var _ discovery.ServerResourcer = discoveryClient{}

// NewDiscoveryClient returns a discoveryClient that reports types available
// to the API Server.
//
// Does not report the scope of each GVK as no tests requiring a discoveryClient
// use scope information.
func NewDiscoveryClient(gvks ...schema.GroupVersionKind) discovery.ServerResourcer {
	gvs := make(map[string][]string)
	for _, gvk := range gvks {
		gv := gvk.GroupVersion().String()
		if _, found := gvs[gv]; !found {
			gvs[gv] = []string{}
		}
		gvs[gv] = append(gvs[gv], gvk.Kind)
	}

	var resources []*metav1.APIResourceList
	for gv, kinds := range gvs {
		resource := &metav1.APIResourceList{
			GroupVersion: gv,
		}
		for _, k := range kinds {
			resource.APIResources = append(resource.APIResources,
				metav1.APIResource{
					Name: strings.ToLower(k) + "s",
					Kind: k,
				})
		}
	}

	return discoveryClient{
		resources: resources,
	}
}
