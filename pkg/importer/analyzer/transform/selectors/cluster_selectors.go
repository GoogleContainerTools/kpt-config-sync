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

package selectors

import (
	clusterregistry "k8s.io/cluster-registry/pkg/apis/clusterregistry/v1alpha1"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// FilterClusters returns the list of Clusters in the passed array of FileObjects.
func FilterClusters(objects []ast.FileObject) ([]clusterregistry.Cluster, status.MultiError) {
	var clusters []clusterregistry.Cluster
	var errs status.MultiError
	for _, object := range objects {
		if object.GetObjectKind().GroupVersionKind() != kinds.Cluster() {
			continue
		}
		if s, err := object.Structured(); err != nil {
			errs = status.Append(errs, err)
		} else {
			o := s.(*clusterregistry.Cluster)
			clusters = append(clusters, *o)
		}
	}
	return clusters, errs
}

// ObjectHasUnknownClusterSelector reports that `resource`'s cluster-selector annotation
// references a ClusterSelector that does not exist.
func ObjectHasUnknownClusterSelector(resource client.Object, annotation string) status.Error {
	return objectHasUnknownSelector.
		Sprintf("Config %q MUST refer to an existing ClusterSelector, but has annotation \"%s=%s\" which maps to no declared ClusterSelector",
			resource.GetName(), metadata.LegacyClusterSelectorAnnotationKey, annotation).
		BuildWithResources(resource)
}
