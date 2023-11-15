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

package controllers

import (
	"fmt"

	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
)

const (
	// RepoSyncBaseClusterRoleName is the namespace reconciler permissions name.
	// e.g. configsync.gke.io:ns-reconciler
	RepoSyncBaseClusterRoleName = configsync.GroupName + ":" + core.NsReconcilerPrefix
	// RootSyncBaseClusterRoleName is the root reconciler base ClusterRole name.
	// e.g. configsync.gke.io:root-reconciler
	RootSyncBaseClusterRoleName = configsync.GroupName + ":" + core.RootReconcilerPrefix
	// RepoSyncBaseRoleBindingName is the name of the default RoleBinding created
	// for RepoSync objects. This contains basic permissions for RepoSync reconcilers
	//(e.g. RepoSync status update).
	RepoSyncBaseRoleBindingName = RepoSyncBaseClusterRoleName
	// RootSyncLegacyClusterRoleBindingName is the name of the legacy ClusterRoleBinding created
	// for RootSync objects. It is always bound to cluster-admin.
	RootSyncLegacyClusterRoleBindingName = RootSyncBaseClusterRoleName
	// RootSyncBaseClusterRoleBindingName is the name of the default ClusterRoleBinding created
	// for RootSync objects. This contains basic permissions for RootSync reconcilers
	// (e.g. RootSync status update).
	RootSyncBaseClusterRoleBindingName = RootSyncBaseClusterRoleName + "-base"
)

// ReconcilerResourceName returns resource name in the format <reconciler-name>-<resource-name>.
func ReconcilerResourceName(reconcilerName, resourceName string) string {
	return fmt.Sprintf("%s-%s", reconcilerName, resourceName)
}
