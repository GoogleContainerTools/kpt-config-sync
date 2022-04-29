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

// NSConfigManagementSystem is the namespace reserved for ACM core components.
const NSConfigManagementSystem = "config-management-system"

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

	// StateStale indicates that the config is different than the last known version from the source
	// of truth.
	StateStale = ConfigSyncState("stale")

	// StateError indicates that there was an error updating the config to match the last known
	// version from the source of truth.
	StateError = ConfigSyncState("error")
)

// SyncState indicates the state of a sync for resources of a particular group and kind.
type SyncState string

const (
	// Syncing indicates these resources are being actively managed by Nomos.
	Syncing SyncState = "syncing"
)

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

// HierarchyModeType defines hierarchical behavior for namespaced objects.
type HierarchyModeType string

const (
	// HierarchyModeInherit indicates that the resource can appear in abstract namespace directories
	// and will be inherited by any descendent namespaces. Without this value on the Sync, resources
	// must not appear in abstract namespaces.
	HierarchyModeInherit = HierarchyModeType("inherit")
	// HierarchyModeNone indicates that the resource cannot appear in abstract namespace directories.
	// For most resource types, this is the same as default, and it's not necessary to specify this
	// value. But RoleBinding and ResourceQuota have different default behaviors, and this value is
	// used to disable inheritance behaviors for those types.
	HierarchyModeNone = HierarchyModeType("none")
	// HierarchyModeDefault is the default value. Default behavior is type-specific.
	HierarchyModeDefault = HierarchyModeType("")
)

// ACM-specific reasons for recorded Kubernetes Events.
const (
	// EventReasonReconcileComplete reports that reconcile succeeded.
	EventReasonReconcileComplete = "ReconcileComplete"
	// EventReasonCRDChange reports that the set of CRDs available on the cluster
	// changed.
	EventReasonCRDChange = "CRDChange"
	// EventReasonStatusUpdateFailed reports that the Syncer was unable to update
	// the status fields of ACM resources.
	EventReasonStatusUpdateFailed = "StatusUpdateFailed"
	// EventReasonInvalidAnnotation reports that there was an issue syncing due to
	// an invalid annotation on a resource.
	EventReasonInvalidAnnotation = "InvalidAnnotation"
	// EventReasonInvalidClusterConfig reports that there is a ClusterConfig on
	// the cluster with an unrecognized name.
	EventReasonInvalidClusterConfig = "InvalidClusterConfig"
	// EventReasonInvalidManagementAnnotation reports that syncing a specific Namespace
	// failed due to it having an invalid management annotation.
	//
	// TODO: Should the reason be "InvalidManagementAnnotation"?
	EventReasonInvalidManagementAnnotation = "InvalidManagementLabel"
	// EventReasonNamespaceCreateFailed reports the syncer was unable to sync
	// a Namespace and its resources due to being unable to create the Namespace.
	EventReasonNamespaceCreateFailed = "NamespaceCreateFailed"
	// EventReasonNamespaceUpdateFailed reports that the syncer was unable to
	// update the resources in a specific Namespace.
	EventReasonNamespaceUpdateFailed = "NamespaceUpdateFailed"
)
