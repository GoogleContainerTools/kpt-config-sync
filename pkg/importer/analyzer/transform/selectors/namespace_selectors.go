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
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjectHasUnknownNamespaceSelector reports that `resource`'s namespace-selector annotation
// references a NamespaceSelector that does not exist.
func ObjectHasUnknownNamespaceSelector(resource client.Object, selector string) status.Error {
	return objectHasUnknownSelector.
		Sprintf("Config %q MUST refer to an existing NamespaceSelector, but has annotation \"%s=%s\" which maps to no declared NamespaceSelector",
			resource.GetName(), metadata.NamespaceSelectorAnnotationKey, selector).
		BuildWithResources(resource)
}

// ObjectNotInNamespaceSelectorSubdirectory reports that `resource` is not in a subdirectory of the directory
// declaring `selector`.
func ObjectNotInNamespaceSelectorSubdirectory(resource client.Object, selector client.Object) status.Error {
	return objectHasUnknownSelector.
		Sprintf("Config %q MUST refer to a NamespaceSelector in its directory or a parent directory. "+
			"Either remove the annotation \"%s=%s\" from %q or move NamespaceSelector %q to a parent directory of %q.",
			resource.GetName(), metadata.NamespaceSelectorAnnotationKey, selector.GetName(), resource.GetName(), selector.GetName(), resource.GetName()).
		BuildWithResources(selector, resource)
}
