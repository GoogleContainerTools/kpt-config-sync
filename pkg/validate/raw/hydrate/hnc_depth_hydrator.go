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
	"strconv"

	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

// HNCDepth hydrates the given Raw objects by annotating each Namespace with its
// depth to be compatible with the Hierarchy Namespace Controller.
func HNCDepth(objs *objects.Raw) status.MultiError {
	for _, obj := range objs.Objects {
		if obj.GetObjectKind().GroupVersionKind() == kinds.Namespace() {
			addDepthLabels(obj)
			originalHNCManagedByValue := core.GetAnnotation(obj, metadata.HNCManagedBy)
			if originalHNCManagedByValue != "" {
				core.SetAnnotation(obj, metadata.OriginalHNCManagedByValue, originalHNCManagedByValue)
			}
			core.SetAnnotation(obj, metadata.HNCManagedBy, metadata.ManagedByValue)
		}
	}
	return nil
}

// addDepthLabels adds depth labels to namespaces from its relative path. For
// example, for "namespaces/foo/bar/namespace.yaml", it will add the following
// two depth labels:
// - "foo.tree.hnc.x-k8s.io/depth: 1"
// - "bar.tree.hnc.x-k8s.io/depth: 0"
func addDepthLabels(obj ast.FileObject) {
	// Relative path for namespaces should start with the "namespaces" directory,
	// include at least one directory matching the name of the namespace, and end
	// with "namespace.yaml". If not, early exit.
	p := obj.Split()
	if len(p) < 3 {
		return
	}

	// Add depth labels for all names in the path except the first "namespaces"
	// directory and the last "namespace.yaml".
	p = p[1 : len(p)-1]

	for i, ans := range p {
		l := ans + metadata.DepthSuffix
		dist := strconv.Itoa(len(p) - i - 1)
		core.SetLabel(obj, l, dist)
	}
}
