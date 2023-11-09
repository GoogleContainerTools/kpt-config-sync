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

package namespaceconfig

import (
	v1 "kpt.dev/configsync/pkg/api/monorepo/v1"
)

// AllConfigs holds things that Importer wants to sync. It is only used in-process, not written
// directly as a Kubernetes resource.
type AllConfigs struct {
	// Map of names to NamespaceConfigs.
	NamespaceConfigs map[string]v1.NamespaceConfig
	// Singleton config for non-CRD cluster-scoped resources.
	ClusterConfig *v1.ClusterConfig
	// Config with declared state for CRDs.
	CRDClusterConfig *v1.ClusterConfig
}
