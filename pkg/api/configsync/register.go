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
	// RepoSyncKind is the kind of the RepoSync resource.
	RepoSyncKind = "RepoSync"
	// RootSyncKind is the kind of the RepoSync resource.
	RootSyncKind = "RootSync"
)

const (
	// DefaultHydrationPollingPeriod is the time delay between polling the
	// filesystem for source updates to render.
	DefaultHydrationPollingPeriod = 5 * time.Second

	// DefaultHydrationRetryPeriod is the time delay between attempts to
	// re-render config after an error.
	// TODO: replace with retry-backoff strategy
	DefaultHydrationRetryPeriod = 30 * time.Minute

	// DefaultReconcilerPollingPeriodSeconds is time delay between polling the
	// filesystem for source updates to sync, in seconds.
	DefaultReconcilerPollingPeriodSeconds = 15

	// DefaultReconcilerPollingPeriod is the time delay between polling the
	// filesystem for source updates to sync.
	DefaultReconcilerPollingPeriod = DefaultReconcilerPollingPeriodSeconds * time.Second

	// DefaultReconcilerResyncPeriod is the time delay between forced re-syncs
	// from source (even without a new commit).
	DefaultReconcilerResyncPeriod = time.Hour

	// DefaultReconcilerRetryPeriod is the time delay between polling the
	// filesystem for source updates to sync, when the previous attempt errored.
	// Note: This retry period is also used for watch updates.
	// TODO: replace with retry-backoff strategy
	DefaultReconcilerRetryPeriod = time.Second

	// DefaultReconcileTimeout is the default wait timeout used by the applier
	// when waiting for reconciliation after actuation.
	// For Apply, it waits for Current status.
	// For Delete, it waits for NotFound status.
	DefaultReconcileTimeout = 5 * time.Minute

	// DefaultHelmReleaseNamespace is the default namespace for a Helm Release which does not have a namespace specified
	DefaultHelmReleaseNamespace = "default"
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
