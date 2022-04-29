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
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/validate/objects"
	"sigs.k8s.io/cli-utils/pkg/common"
)

// PreventDeletion adds the `client.lifecycle.config.k8s.io/deletion: detach` annotation to special namespaces,
// which include `default`, `kube-system`, `kube-public`, `kube-node-lease`, and `gatekeeper-system`.
func PreventDeletion(objs *objects.Raw) status.MultiError {
	for _, obj := range objs.Objects {
		if obj.GetObjectKind().GroupVersionKind().GroupKind() == kinds.Namespace().GroupKind() && differ.SpecialNamespaces[obj.GetName()] {
			core.SetAnnotation(obj, common.LifecycleDeleteAnnotation, common.PreventDeletion)
		}
	}
	return nil
}
