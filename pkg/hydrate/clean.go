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
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/validate/raw/validate"
)

// Clean removes invalid fields from objects before writing them to a file.
func Clean(objects []ast.FileObject) {
	for _, o := range objects {
		clean(o)
	}
}

func clean(o ast.FileObject) {
	annotations := o.GetAnnotations()

	// Restore or remove the hnc.x-k8s.io/managed-by annotation from namespace objects.
	if o.GetObjectKind().GroupVersionKind() == kinds.Namespace() {
		for k := range annotations {
			if k == metadata.HNCManagedBy {
				if value, ok := annotations[metadata.OriginalHNCManagedByValue]; ok {
					annotations[metadata.HNCManagedBy] = value
				} else {
					delete(annotations, k)
				}
			}
		}
	}

	// Remove all the annotations starting with configsync.gke.io or configmanagement.gke.io
	// except for the configmanagement.gke.io/managed annotation.
	for k := range annotations {
		if metadata.HasConfigSyncPrefix(k) && k != metadata.ResourceManagementKey {
			delete(annotations, k)
		}
	}

	if len(annotations) == 0 {
		// Set annotations to nil so that the `annotations` field can be removed from the object metadata.
		annotations = nil
	}
	o.SetAnnotations(annotations)

	labels := o.GetLabels()

	// Remove the <ns>.tree.hnc.x-k8s.io/depth label(s) from namespace objects.
	if o.GetObjectKind().GroupVersionKind() == kinds.Namespace() {
		for k := range labels {
			if validate.HasDepthSuffix(k) {
				delete(labels, k)
			}
		}
	}

	// Remove all the labels starting with configsync.gke.io or configmanagement.gke.io.
	for k := range labels {
		if metadata.HasConfigSyncPrefix(k) {
			delete(labels, k)
		}
	}

	if len(labels) == 0 {
		// Set labels to nil so that the `labels` field can be removed from the object metadata.
		labels = nil
	}
	o.SetLabels(labels)
}
