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

// Package repo contains the user interface definition for the repo structure.
package repo

const (
	// NamespacesDir is the name of the directory containing hierarchical resources.
	NamespacesDir = "namespaces"
	// ClusterDir is the name of the directory containing cluster scoped resources.
	ClusterDir = "cluster"
	// SystemDir is the name of the directory containing Nomos system configuration files.
	SystemDir = "system"
	// ClusterRegistryDir is the name of the directory containing cluster registry and cluster selectors.
	ClusterRegistryDir = "clusterregistry"
	// GCPResourceDir is the name of the directory containing containing GCP resources.
	GCPResourceDir = "hierarchy"
)
