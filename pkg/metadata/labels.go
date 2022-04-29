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
)

// Labels with the `configmanagement.gke.io/` prefix.
const (
	// ManagedByValue marks the resource as managed by Nomos.
	ManagedByValue = configmanagement.GroupName
	// SystemLabel is the system Nomos label.
	SystemLabel = ConfigManagementPrefix + "system"
	// ArchLabel is the arch Nomos label.
	ArchLabel = ConfigManagementPrefix + "arch"
)

// Labels with the `configsync.gke.io/` prefix.
const (
	// ReconcilerLabel is the unique label given to each reconciler pod.
	// This label is set by Config Sync on a root-reconciler or namespace-reconciler pod.
	ReconcilerLabel = configsync.ConfigSyncPrefix + "reconciler"

	// DeclaredVersionLabel declares the API Version in which a resource was initially
	// declared.
	// This label is set by Config Sync on a managed resource.
	DeclaredVersionLabel = configsync.ConfigSyncPrefix + "declared-version"

	// SyncNamespaceLabel indicates the namespace of RootSync or RepoSync.
	SyncNamespaceLabel = configsync.ConfigSyncPrefix + "sync-namespace"

	// SyncNameLabel indicates the name of RootSync or RepoSync.
	SyncNameLabel = configsync.ConfigSyncPrefix + "sync-name"
)

// DepthSuffix is a label suffix for hierarchical namespace depth.
// See definition at http://bit.ly/k8s-hnc-design#heading=h.1wg2oqxxn6ka.
// This label is set by Config Sync on a managed namespace resource.
const DepthSuffix = ".tree.hnc.x-k8s.io/depth"

// ManagedByKey is the recommended Kubernetes label for marking a resource as managed by an
// application.
const ManagedByKey = "app.kubernetes.io/managed-by"

// SyncerLabels returns the Nomos labels that the syncer should manage.
func SyncerLabels() map[string]string {
	return map[string]string{
		ManagedByKey: ManagedByValue,
	}
}
