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

// ReconcilerResourceName returns resource name in the format <reconciler-name>-<resource-name>.
func ReconcilerResourceName(reconcilerName, resourceName string) string {
	return fmt.Sprintf("%s-%s", reconcilerName, resourceName)
}

// RepoSyncPermissionsName returns namespace reconciler permissions name.
// e.g. configsync.gke.io:ns-reconciler
func RepoSyncPermissionsName() string {
	return fmt.Sprintf("%s:%s", configsync.GroupName, core.NsReconcilerPrefix)
}

// RootSyncPermissionsName returns root reconciler permissions name.
// e.g. configsync.gke.io:root-reconciler
func RootSyncPermissionsName() string {
	return fmt.Sprintf("%s:%s", configsync.GroupName, core.RootReconcilerPrefix)
}
