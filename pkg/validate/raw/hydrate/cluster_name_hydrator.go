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
	"kpt.dev/configsync/pkg/validate/fileobjects"
)

// ClusterName annotates the given Raw objects with the name of the current
// cluster.
func ClusterName(objs *fileobjects.Raw) status.MultiError {
	if objs.ClusterName == "" {
		return nil
	}
	for _, obj := range objs.Objects {
		core.Annotation(metadata.ClusterNameAnnotationKey, objs.ClusterName)(obj)
	}
	return nil
}
