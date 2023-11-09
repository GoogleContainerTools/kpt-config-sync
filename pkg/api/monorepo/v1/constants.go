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

package v1

import "kpt.dev/configsync/pkg/api/configmanagement"

// ClusterConfigName is the name of the ClusterConfig for all non-CRD cluster resources.
const ClusterConfigName = "config-management-cluster-config"

// CRDClusterConfigName is the name of the ClusterConfig for CRD resources.
const CRDClusterConfigName = "config-management-crd-cluster-config"

// ConfigSyncState represents the states that a NamespaceConfig or ClusterConfig
// can be in with regards to the source of truth.
type ConfigSyncState string

// IsSynced returns true if the state indicates a config that is synced to the source of truth.
func (p ConfigSyncState) IsSynced() bool {
	return p == StateSynced
}

// IsUnknown returns true if the state is unknown or undeclared.
func (p ConfigSyncState) IsUnknown() bool {
	return p == StateUnknown
}

const (
	// StateUnknown indicates that the config's state is undeclared or unknown.
	StateUnknown = ConfigSyncState("")

	// StateSynced indicates that the config is the same as the last known version from the source of
	// truth.
	StateSynced = ConfigSyncState("synced")
)

// SyncState indicates the state of a sync for resources of a particular group and kind.
type SyncState string

// ResourceConditionState represents the states that a ResourceCondition can be in
type ResourceConditionState string

// IsReconciling returns true if the state is reconciling.
func (p ResourceConditionState) IsReconciling() bool {
	return p == ResourceStateReconciling
}

// IsError returns true if the state is in error.
func (p ResourceConditionState) IsError() bool {
	return p == ResourceStateError
}

const (
	// ResourceStateHealthy indicates a resource with no sync errors found
	ResourceStateHealthy = ResourceConditionState("Healthy")

	// ResourceStateReconciling indicates that a resource is currently being reconciled by a controller
	ResourceStateReconciling = ResourceConditionState("Reconciling")

	// ResourceStateError indicates that an error has occurred while reconciling a resource
	ResourceStateError = ResourceConditionState("Error")
)

// SyncFinalizer is a finalizer handled by Syncer to ensure Sync deletions complete before Importer writes ClusterConfig
// and NamespaceConfig resources.
const SyncFinalizer = "syncer." + configmanagement.GroupName
