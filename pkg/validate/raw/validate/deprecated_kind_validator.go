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
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/importer/analyzer/ast"
	"kpt.dev/configsync/pkg/importer/analyzer/validation/nonhierarchical"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
)

// invalidToValidGroupKinds is a mapping from deprecated GroupKinds to the
// current version of the GroupKind that the config in the repo should be
// replaced with.
var invalidToValidGroupKinds = map[schema.GroupKind]schema.GroupVersionKind{
	v1beta1.SchemeGroupVersion.WithKind("DaemonSet").GroupKind():         kinds.DaemonSet(),
	v1beta1.SchemeGroupVersion.WithKind("Deployment").GroupKind():        kinds.Deployment(),
	v1beta1.SchemeGroupVersion.WithKind("Ingress").GroupKind():           kinds.Ingress(),
	v1beta1.SchemeGroupVersion.WithKind("ReplicaSet").GroupKind():        kinds.ReplicaSet(),
	v1beta1.SchemeGroupVersion.WithKind("NetworkPolicy").GroupKind():     kinds.NetworkPolicy(),
	v1beta1.SchemeGroupVersion.WithKind("PodSecurityPolicy").GroupKind(): kinds.PodSecurityPolicy(),
	v1beta1.SchemeGroupVersion.WithKind("StatefulSet").GroupKind():       kinds.StatefulSet(),
}

// DeprecatedKinds verifies that the given FileObject is not deprecated.
func DeprecatedKinds(obj ast.FileObject) status.Error {
	gk := obj.GetObjectKind().GroupVersionKind().GroupKind()
	if expected, invalid := invalidToValidGroupKinds[gk]; invalid {
		return nonhierarchical.DeprecatedGroupKindError(obj, expected)
	}
	return nil
}
