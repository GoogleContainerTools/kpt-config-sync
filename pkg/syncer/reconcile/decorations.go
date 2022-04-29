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

package reconcile

import (
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SyncedAt marks the resource as synced at the passed sync token.
func SyncedAt(obj client.Object, token string) {
	core.SetAnnotation(obj, metadata.SyncTokenAnnotationKey, token)
}

// enableManagement marks the resource as Nomos-managed.
func enableManagement(obj client.Object) {
	core.SetAnnotation(obj, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
	core.SetAnnotation(obj, metadata.ResourceIDKey, core.GKNN(obj))
	core.SetLabel(obj, metadata.ManagedByKey, metadata.ManagedByValue)
}
