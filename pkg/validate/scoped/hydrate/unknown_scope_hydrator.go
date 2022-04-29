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
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/objects"
)

// UnknownScope hydrates the given Scoped objects by adding an annotation
// `configsync.gke.io/unknown-scope: true` into every object whose scope is
// unknown.
func UnknownScope(objs *objects.Scoped) status.MultiError {
	for _, obj := range objs.Unknown {
		core.SetAnnotation(obj, metadata.UnknownScopeAnnotationKey, metadata.UnknownScopeAnnotationValue)
	}
	return nil
}
