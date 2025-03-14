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

package v1

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// AddResource adds a client.Object to this ClusterConfig.
func (c *ClusterConfig) AddResource(o client.Object) {
	c.Spec.Resources = appendResource(c.Spec.Resources, o)
}

// AddResource adds a client.Object to this NamespaceConfig.
func (c *NamespaceConfig) AddResource(o client.Object) {
	c.Spec.Resources = appendResource(c.Spec.Resources, o)
}

// appendResource adds Object o to resources.
// GenericResources is grouped first by kind and then by version, and this method takes care of
// adding any required groupings for the new object, or adding to existing groupings if present.
func appendResource(resources []GenericResources, o client.Object) []GenericResources {
	gvk := o.GetObjectKind().GroupVersionKind()
	var gr *GenericResources
	for i := range resources {
		if resources[i].Group == gvk.Group && resources[i].Kind == gvk.Kind {
			gr = &resources[i]
			break
		}
	}
	if gr == nil {
		resources = append(resources, GenericResources{
			Group: gvk.Group,
			Kind:  gvk.Kind,
		})
		gr = &resources[len(resources)-1]
	}
	var gvr *GenericVersionResources
	for i := range gr.Versions {
		if gr.Versions[i].Version == gvk.Version {
			gvr = &gr.Versions[i]
			break
		}
	}
	if gvr == nil {
		gr.Versions = append(gr.Versions, GenericVersionResources{
			Version: gvk.Version,
		})
		gvr = &gr.Versions[len(gr.Versions)-1]
	}
	gvr.Objects = append(gvr.Objects, runtime.RawExtension{Object: o})
	return resources
}
