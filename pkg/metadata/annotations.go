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

package metadata

import (
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/kustomize/kyaml/kio/filters"
)

// Annotations with the `configmanagement.gke.io/` prefix.
const (
	// ConfigManagementPrefix is the prefix for all Nomos annotations and labels.
	ConfigManagementPrefix = configmanagement.GroupName + "/"

	// ClusterNameAnnotationKey is the annotation key set on Nomos-managed resources that refers to
	// the name of the cluster that the selectors are applied for.
	// This annotation is set by Config Sync on a managed resource.
	ClusterNameAnnotationKey = ConfigManagementPrefix + "cluster-name"

	// LegacyClusterSelectorAnnotationKey is the annotation key set on Nomos-managed resources that refers
	// to the name of the ClusterSelector resource.
	// This annotation is set by Config Sync users on a managed resource.
	LegacyClusterSelectorAnnotationKey = ConfigManagementPrefix + "cluster-selector"

	// NamespaceSelectorAnnotationKey is the annotation key set on Nomos-managed resources that refers
	// to name of NamespaceSelector resource.
	// This annotation is set by Config Sync users on a managed resource.
	NamespaceSelectorAnnotationKey = ConfigManagementPrefix + "namespace-selector"

	// DeclaredConfigAnnotationKey is the annotation key that stores the declared configuration of
	// a resource in Git.
	// This annotation is set by Config Sync on a managed resource.
	DeclaredConfigAnnotationKey = ConfigManagementPrefix + "declared-config"

	// SourcePathAnnotationKey is the annotation key representing the relative path from POLICY_DIR
	// where the object was originally declared. Paths are slash-separated and OS-agnostic.
	// This annotation is set by Config Sync on a managed resource.
	SourcePathAnnotationKey = ConfigManagementPrefix + "source-path"

	// SyncTokenAnnotationKey is the annotation key representing the last version token that a Nomos-
	// managed resource was successfully synced from.
	// This annotation is set by Config Sync on a managed resource.
	SyncTokenAnnotationKey = ConfigManagementPrefix + "token"

	// The following annotations implement the extended resource status specification.

	// ResourceStatusErrorsKey is the annotation that indicates any errors, encoded as a JSON array.
	// This annotation is set by Config Sync on a managed resource.
	ResourceStatusErrorsKey = ConfigManagementPrefix + "errors"

	// ResourceStatusReconcilingKey is the annotation that indicates reasons why a resource is
	// reconciling, encoded as a JSON array.
	// This annotation is set by Config Sync on a managed resource.
	ResourceStatusReconcilingKey = ConfigManagementPrefix + "reconciling"
)

// Annotations with the `configsync.gke.io/` prefix.
const (
	// ConfigMapAnnotationKey is the annotation key representing the hash of all the configmaps
	// required to run a root-reconciler, namespace-reconciler, or otel-collector pod.
	// This annotation is set by Config Sync on a root-reconciler, namespace-reconciler, or otel-collector pod.
	ConfigMapAnnotationKey = configsync.ConfigSyncPrefix + "configmap"

	// DeclaredFieldsKey is the annotation key that stores the declared configuration of
	// a resource in Git. This uses the same format as the managed fields of server-side apply.
	// This annotation is set by Config Sync on a managed resource.
	DeclaredFieldsKey = configsync.ConfigSyncPrefix + "declared-fields"

	// GitContextKey is the annotation key for the git source-of-truth a resource is synced from.
	// This annotation is set by Config Sync on a managed resource.
	GitContextKey = configsync.ConfigSyncPrefix + "git-context"

	// ResourceManagerKey is the annotation that indicates which multi-repo reconciler is managing
	// the resource.
	// This annotation is set by Config Sync on a managed resource.
	ResourceManagerKey = configsync.ConfigSyncPrefix + "manager"

	// ClusterNameSelectorAnnotationKey is the annotation key set on ConfigSync-managed resources that refers
	// to the name of the ClusterSelector resource.
	// This annotation is set by Config Sync users on a managed resource.
	ClusterNameSelectorAnnotationKey = configsync.ConfigSyncPrefix + "cluster-name-selector"

	// ResourceIDKey is the annotation that indicates the resource's GKNN.
	// This annotation is set by Config  on a managed resource.
	ResourceIDKey = configsync.ConfigSyncPrefix + "resource-id"

	// OriginalHNCManagedByValue is the annotation that stores the original value of the
	// hnc.x-k8s.io/managed-by annotation before Config Sync overrides the annotation.
	// This annotation is set by Config Sync on a managed namespace resource.
	OriginalHNCManagedByValue = configsync.ConfigSyncPrefix + "original-hnc-managed-by-value"

	// UnknownScopeAnnotationKey is the annotation that indicates the scope of a resource is unknown.
	// This annotation is set by Config Sync on a managed resource whose scope is unknown.
	UnknownScopeAnnotationKey = configsync.ConfigSyncPrefix + "unknown-scope"

	// UnknownScopeAnnotationValue is the value for UnknownScopeAnnotationKey
	// to indicate that the scope of a resource is unknown.
	UnknownScopeAnnotationValue = "true"

	// DeletionPropagationPolicyAnnotationKey is the annotation key set on
	// RootSync/RepoSync objects to indicate what do do with the managed
	// resources when the RootSync/RepoSync object is deleted.
	DeletionPropagationPolicyAnnotationKey = configsync.ConfigSyncPrefix + "deletion-propagation-policy"

	// RequiresRenderingAnnotationKey is the annotation key set on
	// RootSync/RepoSync objects to indicate whether the source of truth
	// requires last mile hydration. The reconciler writes the value of this
	// annotation and the reconciler-manager reads it. If set to true, the
	// reconciler-manager will create the reconciler with the hydration-controller
	// sidecar container.
	RequiresRenderingAnnotationKey = configsync.ConfigSyncPrefix + "requires-rendering"

	// DynamicNSSelectorEnabledAnnotationKey is the annotation key set on R*Sync
	// object to indicate whether the source of truth contains at least one
	// NamespaceSelector using the dynamic mode, which requires the Namespace
	// controller. The reconciler writes the value of this annotation and the
	// reconciler-manager reads it. If set to true, the reconciler-manager will
	// create the reconciler with the Namespace controller in the reconciler container.
	DynamicNSSelectorEnabledAnnotationKey = configsync.ConfigSyncPrefix + "dynamic-ns-selector-enabled"

	// ImageToSyncAnnotationKey is the annotation key used to store the full image reference
	// (including the digest) for OCI and Helm (with oci:// URL) sources.
	// This annotation is set by Config Sync on the RootSync/RepoSync object
	// to indicate the exact image that should be synced.
	ImageToSyncAnnotationKey = configsync.ConfigSyncPrefix + "image-to-sync"

	// StatusModeAnnotationKey annotates a ResourceGroup CR
	// to communicate with the ResourceGroup controller.
	// When the value is set to "disabled", the ResourceGroup controller
	// ignores the ResourceGroup CR.
	StatusModeAnnotationKey = configsync.ConfigSyncPrefix + "status"
)

// Lifecycle annotations
const (
	// LifecyclePrefix is the prefix for all lifecycle annotations.
	LifecyclePrefix = "client.lifecycle.config.k8s.io"

	// LifecycleMutationAnnotation is the lifecycle annotation key for the mutation
	// operation. The annotation must be declared in the repository in order to
	// function properly. This annotation only has effect when the object
	// updated in the cluster or the declaration changes. It has no impact on
	// behavior related to object creation/deletion, or if the object does not
	// already exist.
	// This annotation is set by Config Sync users on a managed resource.
	LifecycleMutationAnnotation = LifecyclePrefix + "/mutation"

	// IgnoreMutation is the value used with LifecycleMutationAnnotation to
	// prevent mutating a resource. That is, if the resource exists on the cluster
	// then ACM will make no attempt to modify it.
	IgnoreMutation = "ignore"
)

// OwningInventoryKey is the annotation key for marking the owning-inventory object.
// This annotation is needed because the kpt library cannot apply a single resource.
// This annotation is set by Config Sync on a managed resource.
const OwningInventoryKey = inventory.OwningInventoryKey

// HNCManagedBy is the annotation that indicates the namespace hierarchy is
// not managed by the Hierarchical Namespace Controller (http://bit.ly/k8s-hnc-design) but
// someone else, "configmanagement.gke.io" in this case.
// This annotation is set by Config Sync on a managed namespace resource.
const HNCManagedBy = "hnc.x-k8s.io/managed-by"

// ConfigSyncAnnotations contain the keys for ConfigSync annotations.
var ConfigSyncAnnotations = []string{
	DeclaredFieldsKey,
	GitContextKey,
	ResourceManagerKey,
	ResourceIDKey,
}

// Annotation for local configuration
const (
	// LocalConfigAnnotationKey is the annotation key to mark
	// a resource is only local. When its value is "true",
	// the resource shouldn't be applied to the cluster.
	// This annotation is set by Config Sync users on a resource that
	// should be only used by local tools such as kpt function.
	LocalConfigAnnotationKey = filters.LocalConfigAnnotation

	// Any value except for NoLocalConfigAnnoVal will mark a resource as a local configuration.
	NoLocalConfigAnnoVal = "false"
)

// AutoPilotAnnotation is the annotation generated by the autopilot for resource adjustment.
const AutoPilotAnnotation = "autopilot.gke.io/resource-adjustment"

// KustomizeOrigin is the annotation generated by Kustomize to indicate the origin of the rendered resource.
const KustomizeOrigin = "config.kubernetes.io/origin"

// FleetWorkloadIdentityCredentials is the key for the credentials file of the Fleet Workload Identity.
const FleetWorkloadIdentityCredentials = "config.kubernetes.io/fleet-workload-identity"

// DeletionPropagationPolicy is the type used to identify value enums to use
// with the deletion-propagation-policy annotation.
type DeletionPropagationPolicy string

// String returns the string value of the DeletionPropagationPolicy.
// Implements the Stringer interface.
func (p DeletionPropagationPolicy) String() string {
	return string(p)
}

const (
	// DeletionPropagationPolicyForeground indicates that the managed resources
	// should all be deleted/pruned before the RootSync/RepoSync object is deleted.
	// This will block deletion of the RootSync/RepoSync using a finalizer.
	DeletionPropagationPolicyForeground = DeletionPropagationPolicy("Foreground")

	// DeletionPropagationPolicyOrphan indicates that the managed resources
	// should all be orphanned (not deleted) when the RootSync/RepoSync object
	// is deleted.
	// This will NOT block deletion of the RootSync/RepoSync AND will not
	// remove or modify any config sync managed annotations.
	// This allows the RootSync/RepoSync to be deleted and re-created without
	// affecting the managed resources.
	// This is the default behavior if the annotation is not specified.
	DeletionPropagationPolicyOrphan = DeletionPropagationPolicy("Orphan")
)

// StatusMode is the type used to identify value enums to use with the
// `configsync.gke.io/status` annotation.
type StatusMode string

// String returns the string value of the StatusMode.
// Implements the Stringer interface.
func (m StatusMode) String() string {
	return string(m)
}

const (
	// StatusEnabled is used to enable writing status to the ResourceGroup
	// inventory.
	StatusEnabled StatusMode = "enabled"
	// StatusDisabled is used to disabled writing status to the ResourceGroup
	// inventory. This is useful as part of a mitigation strategy when the
	// status pushes the object size over the etcd entry size limit. Disabling
	// the status allows syncing to succeed while you reduce the number of
	// objects in the source of truth or split the apply set across multiple
	// RSyncs.
	StatusDisabled StatusMode = "disabled"
)
