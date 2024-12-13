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

package syncertest

import (
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Token is a test sync token.
const Token = "b38239ea8f58eaed17af6734bd6a025eeafccda1"

var (
	// ManagementEnabled sets management labels and annotations on the object.
	ManagementEnabled core.MetaMutator = func(obj client.Object) {
		core.SetAnnotation(obj, metadata.ResourceManagementKey, metadata.ResourceManagementEnabled)
		core.SetAnnotation(obj, metadata.ResourceIDKey, core.GKNN(obj))
		core.SetLabel(obj, metadata.ManagedByKey, metadata.ManagedByValue)
	}
	// ManagementDisabled sets the management disabled annotation on the object.
	ManagementDisabled = core.Annotation(metadata.ResourceManagementKey, metadata.ResourceManagementDisabled)
	// ManagementInvalid sets an invalid management annotation on the object.
	ManagementInvalid = core.Annotation(metadata.ResourceManagementKey, "invalid")
	// TokenAnnotation sets the sync token annotation on the object
	TokenAnnotation = core.Annotation(metadata.SyncTokenAnnotationKey, Token)
	// IgnoreMutationAnnotation sets the ignore mutation annotation on the object
	IgnoreMutationAnnotation = core.Annotation(metadata.LifecycleMutationAnnotation, metadata.IgnoreMutation)
)
