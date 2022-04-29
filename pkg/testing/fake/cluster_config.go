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

// ClusterConfigMutator mutates a ClusterConfig.
type ClusterConfigMutator func(cc *v1.ClusterConfig)

// ClusterConfigMeta wraps a MetaMutator for modifying ClusterConfigs.
func ClusterConfigMeta(opts ...core.MetaMutator) ClusterConfigMutator {
	return func(cc *v1.ClusterConfig) {
		mutate(cc, opts...)
	}
}

// CRDClusterConfigObject initializes a valid CRDClusterConfig.
func CRDClusterConfigObject(opts ...ClusterConfigMutator) *v1.ClusterConfig {
	mutators := append(opts, ClusterConfigMeta(core.Name(v1.CRDClusterConfigName)))
	return ClusterConfigObject(mutators...)
}

// ClusterConfigObject initializes a ClusterConfig.
func ClusterConfigObject(opts ...ClusterConfigMutator) *v1.ClusterConfig {
	result := &v1.ClusterConfig{TypeMeta: ToTypeMeta(kinds.ClusterConfig())}
	defaultMutate(result)
	mutate(result, core.Name(v1.ClusterConfigName))
	for _, opt := range opts {
		opt(result)
	}

	return result
}
