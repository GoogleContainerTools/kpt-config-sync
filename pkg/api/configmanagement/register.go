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

package configmanagement

const (
	// CLIName is the short name of the CLI.
	CLIName = "nomos"

	// MetricsNamespace is the namespace that metrics are held in.
	MetricsNamespace = "gkeconfig"

	// GroupName is the name of the group of configmanagement resources.
	GroupName = "configmanagement.gke.io"

	// ProductName is what we call Nomos externally.
	ProductName = "Anthos Configuration Management"

	// ControllerNamespace is the Namespace used for Nomos controllers
	ControllerNamespace = "config-management-system"

	// OperatorKind is the Kind of the Operator config object.
	OperatorKind = "ConfigManagement"

	// SyncKind is the string constant for the Sync GroupVersionKind
	SyncKind = "Sync"

	// RepoKind is the string constant for the Repo GroupVersionKind
	RepoKind = "Repo"

	// ClusterSelectorKind is the string constant for the ClusterSelector GroupVersionKind
	ClusterSelectorKind = "ClusterSelector"

	// NamespaceSelectorKind is the string constant for the NamespaceSelector GroupVersionKind
	NamespaceSelectorKind = "NamespaceSelector"

	// NamespaceConfigKind is the string constant for the NamespaceConfig GroupVersionKind
	NamespaceConfigKind = "NamespaceConfig"

	// ClusterConfigKind is the string constant for the ClusterConfig GroupVersionKind
	ClusterConfigKind = "ClusterConfig"

	// HierarchyConfigKind is the string constant for the HierarchyConfig GroupVersionKind
	HierarchyConfigKind = "HierarchyConfig"
)

// IsControllerNamespace returns true if the namespace is the ACM Controller Namespace.
func IsControllerNamespace(name string) bool {
	// For now we only forbid syncing the Namespace containing the ACM controllers.
	return name == ControllerNamespace
}
