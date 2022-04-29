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

package nonhierarchical

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NameCollisionErrorCode is the error code for ObjectNameCollisionError
const NameCollisionErrorCode = "1029"

// nameCollisionErrorBuilder is
var nameCollisionErrorBuilder = status.NewErrorBuilder(NameCollisionErrorCode)

// NamespaceCollisionError reports multiple declared Namespaces with the same name.
func NamespaceCollisionError(name string, duplicates ...client.Object) status.Error {
	return nameCollisionErrorBuilder.
		Sprintf("Namespaces MUST have unique names. Found %d Namespaces named %q. Rename or merge the Namespaces to fix:",
			len(duplicates), name).
		BuildWithResources(duplicates...)
}

// NamespaceMetadataNameCollisionError reports that multiple namespace-scoped objects of the same Kind and
// namespace have the same metadata name
func NamespaceMetadataNameCollisionError(gk schema.GroupKind, namespace string, name string, duplicates ...client.Object) status.Error {
	return nameCollisionErrorBuilder.
		Sprintf("Namespace-scoped configs of the same Group and Kind MUST have unique names if they are in the same Namespace. "+
			"Found %d configs of GroupKind %q in Namespace %q named %q. Rename or delete the duplicates to fix:",
			len(duplicates), gk.String(), namespace, name).
		BuildWithResources(duplicates...)
}

// ClusterMetadataNameCollisionError reports that multiple cluster-scoped objects of the same Kind and
// namespace have the same metadata.name.
func ClusterMetadataNameCollisionError(gk schema.GroupKind, name string, duplicates ...client.Object) status.Error {
	return nameCollisionErrorBuilder.
		Sprintf("Cluster-scoped configs of the same Group and Kind MUST have unique names. "+
			"Found %d configs of GroupKind %q named %q. Rename or delete the duplicates to fix:",
			len(duplicates), gk.String(), name).
		BuildWithResources(duplicates...)
}

// SelectorMetadataNameCollisionError reports that multiple ClusterSelectors or NamespaceSelectors
// have the same metadata.name.
func SelectorMetadataNameCollisionError(kind string, name string, duplicates ...client.Object) status.Error {
	return nameCollisionErrorBuilder.
		Sprintf("%ss MUST have globally-unique names. "+
			"Found %d %ss named %q. Rename or delete the duplicates to fix:",
			kind, len(duplicates), kind, name).
		BuildWithResources(duplicates...)
}
