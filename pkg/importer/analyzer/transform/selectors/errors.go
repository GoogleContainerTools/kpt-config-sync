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

// ObjectHasUnknownSelectorCode is the error code for ObjectHasUnknownClusterSelector
const ObjectHasUnknownSelectorCode = "1013"

var objectHasUnknownSelector = status.NewErrorBuilder(ObjectHasUnknownSelectorCode)

// InvalidSelectorErrorCode is the error code for InvalidSelectorError
const InvalidSelectorErrorCode = "1014" // TODO: Must refactor to use properly

var invalidSelectorError = status.NewErrorBuilder(InvalidSelectorErrorCode)

// InvalidSelectorError reports that a ClusterSelector or NamespaceSelector is
// invalid.
// To be renamed in refactoring that removes above error.
func InvalidSelectorError(selector client.Object, cause error) status.Error {
	return invalidSelectorError.Sprintf("%s has validation errors that must be corrected", selector.GetObjectKind().GroupVersionKind().Kind).Wrap(cause).BuildWithResources(selector)
}

// EmptySelectorError reports that a ClusterSelector or NamespaceSelector is
// invalid because it is empty.
func EmptySelectorError(selector client.Object) status.Error {
	return invalidSelectorError.Sprintf("%ss MUST define `spec.selector`", selector.GetObjectKind().GroupVersionKind().Kind).BuildWithResources(selector)
}

// ClusterSelectorAnnotationConflictErrorCode is the error code for ClusterSelectorAnnotationConflictError
const ClusterSelectorAnnotationConflictErrorCode = "1066"

var clusterSelectorAnnotationConflict = status.NewErrorBuilder(ClusterSelectorAnnotationConflictErrorCode)

// ClusterSelectorAnnotationConflictError reports that an object has both the legacy cluster-selector annotation and the inline annotation.
func ClusterSelectorAnnotationConflictError(resource client.Object) status.Error {
	return clusterSelectorAnnotationConflict.Sprintf(
		"Config %q MUST declare ONLY ONE cluster-selector annotation, but has both inline annotation %q and legacy annotation %q. "+
			"To fix, remove one of the annotations from:", resource.GetName(),
		metadata.ClusterNameSelectorAnnotationKey, metadata.LegacyClusterSelectorAnnotationKey).BuildWithResources(resource)
}
