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

package reconcilermanager

const (
	// ManagerName is the name of the controller which creates reconcilers.
	ManagerName = "reconciler-manager"
)

const (
	// SourceFormat is the key used for storing whether a repository is
	// unstructured or in hierarchy mode. Used in many objects related to this
	// behavior.
	SourceFormat = "source-format"

	// ClusterNameKey is the OS env variable key for the name
	// of the cluster.
	ClusterNameKey = "CLUSTER_NAME"

	// ScopeKey is the OS env variable key for the scope of the
	// reconciler and hydration controller.
	ScopeKey = "SCOPE"

	// SyncNameKey is the OS env variable key for the name of
	// the RootSync or RepoSync object.
	SyncNameKey = "SYNC_NAME"

	// ReconcilerNameKey is the OS env variable key for the name of
	// the Reconciler Deployment.
	ReconcilerNameKey = "RECONCILER_NAME"

	// NamespaceNameKey is the OS env variable key for the name of
	// the Reconciler's namespace
	NamespaceNameKey = "NAMESPACE_NAME"

	// SyncDirKey is the OS env variable key for the sync directory
	// read by the hydration controller and the reconciler.
	SyncDirKey = "SYNC_DIR"

	// GitSync is the name of the git-sync container in reconciler pods.
	GitSync = "git-sync"

	// OciSync is the name of the oci-sync container in reconciler pods.
	OciSync = "oci-sync"

	// HydrationController is the name of the hydration-controller container in reconciler pods.
	HydrationController = "hydration-controller"

	//HydrationControllerWithShell is the name of the hydration-controller image that has shell
	HydrationControllerWithShell = "hydration-controller-with-shell"

	// Reconciler is a common building block for many resource names associated
	// with reconciling resources.
	Reconciler = "reconciler"

	// ReconcileTimeout is to control the kpt applier reconcile/prune task timeout
	ReconcileTimeout = "RECONCILE_TIMEOUT"

	// StatusMode is to control if the kpt applier needs to inject the actuation data
	// into the ResourceGroup object.
	StatusMode = "STATUS_MODE"
)

const (
	// SourceTypeKey is the OS env variable key for the type of the source repo, must be git or oci.
	SourceTypeKey = "SOURCE_TYPE"

	// SourceRepoKey is the OS env variable key for the git or OCI repo URL.
	SourceRepoKey = "SOURCE_REPO"

	// SourceBranchKey is the OS env variable key for the git branch name. It doesn't apply to OCI.
	SourceBranchKey = "SOURCE_BRANCH"

	// SourceRevKey is the OS env variable key for the git revision. It doesn't apply to OCI.
	SourceRevKey = "SOURCE_REV"
)

const (
	// ReconcilerPollingPeriod defines how often the reconciler should poll the
	// filesystem for updates to the source or rendered configs.
	ReconcilerPollingPeriod = "RECONCILER_POLLING_PERIOD"

	// HydrationPollingPeriod defines how often the hydration controller should
	// poll the filesystem for rendering the DRY configs.
	HydrationPollingPeriod = "HYDRATION_POLLING_PERIOD"
)

const (
	// OciSyncImage is the OS env variable key for the OCI image URL.
	OciSyncImage = "OCI_SYNC_IMAGE"

	// OciSyncAuth is the OS env variable key for the OCI sync auth type.
	OciSyncAuth = "OCI_SYNC_AUTH"

	// OciSyncWait is the OS env variable key for the OCI sync wait period in seconds.
	OciSyncWait = "OCI_SYNC_WAIT"
)
