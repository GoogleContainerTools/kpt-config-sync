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

import "kpt.dev/configsync/pkg/api/configsync"

const (
	// ManagerName is the name of the controller which creates reconcilers.
	ManagerName = "reconciler-manager"

	// FieldManager is the field manager used by the reconciler-manager.
	// This avoids conflicts between the reconciler and reconciler-manager.
	FieldManager = configsync.ConfigSyncPrefix + ManagerName
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

	// SyncGenerationKey is the OS env variable key for the generation of
	// the RootSync or RepoSync object.
	SyncGenerationKey = "SYNC_GENERATION"

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

	// GCENodeAskpassSidecar is the name of the gcenode-askpass-sidecar container in reconciler pods.
	GCENodeAskpassSidecar = "gcenode-askpass-sidecar"

	// OciSync is the name of the oci-sync container in reconciler pods.
	OciSync = "oci-sync"

	// HelmSync is the name of the helm-sync container in reconciler pods.
	HelmSync = "helm-sync"

	// HydrationController is the name of the hydration-controller container in reconciler pods.
	HydrationController = "hydration-controller"

	//HydrationControllerWithShell is the name of the hydration-controller image that has shell
	HydrationControllerWithShell = "hydration-controller-with-shell"

	// Reconciler is a common building block for many resource names associated
	// with reconciling resources.
	Reconciler = "reconciler"

	// ReconcileTimeout is to control the kpt applier reconcile/prune task timeout
	ReconcileTimeout = "RECONCILE_TIMEOUT"

	// APIServerTimeout is to control the client-side timeout when talking to the API server
	APIServerTimeout = "API_SERVER_TIMEOUT"

	// StatusMode is to control if the kpt applier needs to inject the actuation data
	// into the ResourceGroup object.
	StatusMode = "STATUS_MODE"

	// RenderingEnabled tells the reconciler container whether the hydration-controller
	// container is running in the Pod.
	RenderingEnabled = "RENDERING_ENABLED"

	// NamespaceStrategy tells the reconciler container which NamespaceStrategy to
	// use
	NamespaceStrategy = "NAMESPACE_STRATEGY"

	// DynamicNSSelectorEnabled tells the reconciler container whether the dynamic
	// mode is enabled in NamespaceSelectors, which requires a Namespace controller
	// to be running.
	DynamicNSSelectorEnabled = "DYNAMIC_NS_SELECTOR_ENABLED"

	// WebhookEnabled tells the reconciler container whether the Admission Webhook
	// is installed and running on the cluster.
	WebhookEnabled = "WEBHOOK_ENABLED"
)

const (
	// SourceTypeKey is the OS env variable key for the type of the source repo, must be git or oci or helm.
	SourceTypeKey = "SOURCE_TYPE"

	// SourceRepoKey is the OS env variable key for the git or OCI or Helm repo URL.
	SourceRepoKey = "SOURCE_REPO"

	// SourceBranchKey is the OS env variable key for the git branch name. It doesn't apply to OCI and helm.
	SourceBranchKey = "SOURCE_BRANCH"

	// SourceRevKey is the OS env variable key for the git or helm revision.
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

	// OciCACert is the OS env variable key for the OCI CA cert file path.
	// This variable is consumed by the underlying crypto library:
	// - https://pkg.go.dev/crypto/x509#SystemCertPool
	OciCACert = "SSL_CERT_FILE"
)

const (
	// HelmRepo is the OS env variable key for the Helm repository URL.
	HelmRepo = "HELM_REPO"

	// HelmChart is the OS env variable key for the Helm chart name.
	HelmChart = "HELM_CHART"

	// HelmChartVersion is the OS env variable key for the Helm chart version.
	HelmChartVersion = "HELM_CHART_VERSION"

	// HelmReleaseName is the OS env variable key for the Helm release name.
	HelmReleaseName = "HELM_RELEASE_NAME"

	//HelmReleaseNamespace is the OS env variable key for the Helm release namespace.
	HelmReleaseNamespace = "HELM_RELEASE_NAMESPACE"

	//HelmDeployNamespace is the OS env variable key for the Helm deploy namespace.
	HelmDeployNamespace = "HELM_DEPLOY_NAMESPACE"

	// HelmValuesYAML is the OS env variable key for the inline Helm chart values, formatted as a yaml
	// string in the same format as the default values.yaml accompanying the chart.
	HelmValuesYAML = "HELM_VALUES_YAML"

	// HelmValuesFilePaths is the OS env variable key for a comma-separated list of all the valuesFile paths
	// that were mounted from ConfigMaps.
	HelmValuesFilePaths = "HELM_VALUES_FILE_PATHS"

	//HelmIncludeCRDs is the OS env variable key for whether to include CRDs in helm rendering output.
	HelmIncludeCRDs = "HELM_INCLUDE_CRDS"

	//HelmAuthType is the OS env variable key for Helm sync auth type.
	HelmAuthType = "HELM_AUTH_TYPE"

	// HelmSyncWait is the OS env variable key for the Helm sync wait period in seconds.
	HelmSyncWait = "HELM_SYNC_WAIT"

	// HelmCACert is the OS env variable key for the Helm sync CA cert file path.
	HelmCACert = "HELM_CA_CERT"
)

const (
	// MaxRepoSyncNNLength length is the maximum number of characters for a RepoSync name + namespace.
	// The ns-reconciler deployment label is constructed as "ns-reconciler-<ns>-<name>-<len(name)>"
	// Thus, the max length name + namespace for a RepoSync is 45 characters.
	MaxRepoSyncNNLength = 45
	// MaxRootSyncNameLength length is the maximum number of characters for a RootSync name.
	// The name max length is 63 - len("config-management-system_") due to the inventory-id label.
	// Thus, the max length name for a RootSync is 38 characters.
	MaxRootSyncNameLength = 38
)

// these constants are kept here to avoid import cycle
const (
	// GitSecretGithubAppApplicationID is the key at which the githubapp app id is stored
	GitSecretGithubAppApplicationID = "github-app-application-id"
	// GitSecretGithubAppClientID is the key at which the githubapp client id is stored
	GitSecretGithubAppClientID = "github-app-client-id"
)
