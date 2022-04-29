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
	"fmt"
	"path/filepath"
	"strings"

	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/filesystem/cmpath"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// generateUniqueFileNames sets a default file path for each object, guaranteed to be unique for a collection
// of ast.FileObjects which do not collide (group/kind/namespace/name duplication)
func generateUniqueFileNames(extension string, multiCluster bool, objects ...ast.FileObject) []ast.FileObject {
	duplicates := make(map[string]int, len(objects))
	for i := range objects {
		p := cmpath.RelativeSlash(filename(extension, objects[i], multiCluster, false))
		objects[i].Relative = p
		duplicates[p.SlashPath()]++
	}

	for i, obj := range objects {
		if duplicates[obj.SlashPath()] > 1 {
			objects[i] = ast.NewFileObject(obj.Unstructured, cmpath.RelativeSlash(filename(extension, obj.Unstructured, multiCluster, true)))
		}
	}

	return objects
}

func filename(extension string, o client.Object, includeCluster bool, includeGroup bool) string {
	gvk := o.GetObjectKind().GroupVersionKind()
	var path string
	if includeGroup {
		path = fmt.Sprintf("%s.%s_%s.%s", gvk.Kind, gvk.Group, o.GetName(), extension)
	} else {
		path = fmt.Sprintf("%s_%s.%s", gvk.Kind, o.GetName(), extension)
	}
	if namespace := o.GetNamespace(); namespace != "" {
		path = filepath.Join(namespace, path)
	}
	if includeCluster {
		if clusterName, found := o.GetAnnotations()[metadata.ClusterNameAnnotationKey]; found {
			path = filepath.Join(clusterName, path)
		} else {
			path = filepath.Join(defaultCluster, path)
		}
	}
	return strings.ToLower(path)
}
