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

package fake

import (
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
)

// NamespaceConfigObject initializes a NamespaceConfig.
func NamespaceConfigObject(opts ...core.MetaMutator) *v1.NamespaceConfig {
	result := &v1.NamespaceConfig{TypeMeta: ToTypeMeta(kinds.NamespaceConfig())}
	defaultMutate(result)
	for _, opt := range opts {
		opt(result)
	}

	return result
}
