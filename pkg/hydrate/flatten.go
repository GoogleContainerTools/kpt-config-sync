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

package hydrate

import (
	corev1 "k8s.io/api/core/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/util/namespaceconfig"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Flatten converts an AllConfigs into a list of FileObjects.
func Flatten(c *namespaceconfig.AllConfigs) []client.Object {
	var result []client.Object
	if c == nil {
		return result
	}

	// Flatten with default filenames.
	if c.CRDClusterConfig != nil {
		for _, crds := range c.CRDClusterConfig.Spec.Resources {
			result = append(result, resourcesToFileObjects(crds)...)
		}
	}
	if c.ClusterConfig != nil {
		for _, clusterObjects := range c.ClusterConfig.Spec.Resources {
			result = append(result, resourcesToFileObjects(clusterObjects)...)
		}
	}
	if c.NamespaceConfigs != nil {
		for _, namespaceCfg := range c.NamespaceConfigs {
			// Construct Namespace from NamespaceConfig
			namespace := &corev1.Namespace{}
			namespace.SetGroupVersionKind(kinds.Namespace())
			// Note that this copies references to Annotations/Labels.
			namespace.ObjectMeta = namespaceCfg.ObjectMeta
			result = append(result, namespace)

			for _, namespaceObjects := range namespaceCfg.Spec.Resources {
				result = append(result, resourcesToFileObjects(namespaceObjects)...)
			}
		}
	}

	return result
}

// resourcesToFileObjects flattens a GenericResources into a list of FileObjects.
func resourcesToFileObjects(r v1.GenericResources) []client.Object {
	var result []client.Object

	for _, version := range r.Versions {
		for _, raw := range version.Objects {
			// We assume a GenericResources will only hold KubernetesObjects, and never Lists.
			result = append(result, raw.Object.(client.Object))
		}
	}

	return result
}
