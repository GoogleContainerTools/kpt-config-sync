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

package validate

import (
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespaceSelectors validates that all NamespaceSelectors have a unique name.
func NamespaceSelectors(objs *objects.Scoped) status.MultiError {
	var errs status.MultiError
	gk := kinds.NamespaceSelector().GroupKind()
	matches := make(map[string][]client.Object)

	for _, obj := range objs.Cluster {
		if obj.GetObjectKind().GroupVersionKind().GroupKind() == gk {
			matches[obj.GetName()] = append(matches[obj.GetName()], obj)
		}
	}

	for name, duplicates := range matches {
		if len(duplicates) > 1 {
			errs = status.Append(errs, nonhierarchical.SelectorMetadataNameCollisionError(gk.Kind, name, duplicates...))
		}
	}

	return errs
}
