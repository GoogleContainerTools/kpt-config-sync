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

package configsync

import "time"

const (
	// GroupName is the name of the group of configsync resources.
	GroupName = "configsync.gke.io"

	// ConfigSyncPrefix is the prefix for all ConfigSync annotations and labels.
	ConfigSyncPrefix = GroupName + "/"

	// FieldManager is the field manager name for server-side apply.
	FieldManager = GroupName

	// ControllerNamespace is the Namespace used for Nomos controllers
	ControllerNamespace = "config-management-system"
)

// API type constants
const (
	// RepoSyncName is the expected name of any RepoSync CR.
	RepoSyncName = "repo-sync"
	// RootSyncName is the expected name of any RootSync CR.
	RootSyncName = "root-sync"
)

const (
	// DefaultPeriodSecs is the default value in seconds between consecutive syncs.
	DefaultPeriodSecs = 15

	// DefaultReconcilerPollingPeriod defines how often the reconciler should poll
	// the filesystem for updates to the source or rendered configs.
	DefaultReconcilerPollingPeriod = 5 * time.Second

	// DefaultHydrationPollingPeriod defines how often the hydration controller
	// should poll the filesystem for rendering the DRY configs.
	DefaultHydrationPollingPeriod = 5 * time.Second

	// DefaultReconcileTimeout defines the timeout of kpt applier reconcile/prune task
	DefaultReconcileTimeout = 5 * time.Minute
)

// AuthType specifies the type to authenticate to a repository.
type AuthType string

const (
	// AuthGCENode indicates using gcenode to authenticate to Git or OCI.
	AuthGCENode AuthType = "gcenode"
	// AuthSSH indicates using an ssh key to authenticate to Git. It doesn't apply to OCI.
	AuthSSH AuthType = "ssh"
	// AuthCookieFile indicates using cookiefile to authenticate to Git. It doesn't apply to OCI.
	AuthCookieFile AuthType = "cookiefile"
	// AuthNone indicates no auth token is required for Git or OCI or Helm.
	AuthNone AuthType = "none"
	// AuthToken indicates using a username/password to authenticate to Git or Helm. It doesn't apply to OCI.
	AuthToken AuthType = "token"
	// AuthGCPServiceAccount indicates using a GCP service account to authenticate to
	// Git or OCI or Helm, when GKE Workload Identity or Fleet Workload Identity is enabled.
	AuthGCPServiceAccount AuthType = "gcpserviceaccount"
)
