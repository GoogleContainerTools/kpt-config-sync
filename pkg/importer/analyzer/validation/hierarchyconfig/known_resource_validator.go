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

package hierarchyconfig

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ClusterScopedResourceInHierarchyConfigErrorCode is the error code for ClusterScopedResourceInHierarchyConfigError
const ClusterScopedResourceInHierarchyConfigErrorCode = "1046"

var clusterScopedResourceInHierarchyConfigError = status.NewErrorBuilder(ClusterScopedResourceInHierarchyConfigErrorCode)

// ClusterScopedResourceInHierarchyConfigError reports that a Resource defined in a HierarchyConfig
// has Cluster scope which means it's not feasible to interpret the resource in a hierarchical
// manner
func ClusterScopedResourceInHierarchyConfigError(config client.Object, gk schema.GroupKind) status.Error {
	return clusterScopedResourceInHierarchyConfigError.
		Sprintf("This HierarchyConfig references the APIResource %q which has Cluster scope. "+
			"Cluster scoped objects are not permitted in HierarchyConfig.",
			gk.String()).
		BuildWithResources(config)
}
