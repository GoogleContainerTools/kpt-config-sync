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

package metrics

import (
	"go.opencensus.io/tag"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
)

var (
	// KeyName groups metrics by the reconciler name. Possible values: root-reconciler, ns-reconciler-<namespace>
	// TODO b/208316928 remove this key from pipeline_error_observed metric once same metric in Resource Group Controller has this tag removed
	KeyName, _ = tag.NewKey("name")

	// KeyReconcilerType groups metrics by the reconciler type. Possible values: root, namespace.
	// TODO: replace with configsync.sync.kind resource attribute
	KeyReconcilerType, _ = tag.NewKey("reconciler")

	// KeyOperation groups metrics by their operation. Possible values: create, patch, update, delete.
	KeyOperation, _ = tag.NewKey("operation")

	// KeyController groups metrics by their controller. Possible values: applier, remediator.
	KeyController, _ = tag.NewKey("controller")

	// KeyComponent groups metrics by their component. Possible values: source, sync, rendering, readiness(from Resource Group Controller).
	KeyComponent, _ = tag.NewKey("component")

	// KeyErrorClass groups metrics by their error code.
	KeyErrorClass, _ = tag.NewKey("errorclass")

	// KeyStatus groups metrics by their status. Possible values: success, error.
	KeyStatus, _ = tag.NewKey("status")

	// KeyType groups metrics by their resource GVK.
	KeyType, _ = tag.NewKey("type")

	// KeyInternalErrorSource groups the InternalError metrics by their source. Possible values: parser, differ, remediator.
	KeyInternalErrorSource, _ = tag.NewKey("source")

	// KeyParserSource groups the metrics for the parser by their source. Possible values: read, parse, update.
	KeyParserSource, _ = tag.NewKey("source")

	// KeyTrigger groups metrics by their trigger. Possible values: retry, watchUpdate, managementConflict, resync, reimport.
	KeyTrigger, _ = tag.NewKey("trigger")

	// KeyCommit groups metrics by their git commit. Even though this tag has a high cardinality,
	// it is only used by the `last_sync_timestamp` and `last_apply_timestamp` metrics.
	// These are both aggregated as LastValue metrics so the number of recorded values will always be
	// at most 1 per git commit.
	KeyCommit, _ = tag.NewKey("commit")

	// KeyContainer groups metrics by their container names. Possible values: reconciler, git-sync.
	// TODO: replace with k8s.container.name resource attribute
	KeyContainer, _ = tag.NewKey("container")

	// KeyResourceType groups metris by their resource types. Possible values: cpu, memory.
	KeyResourceType, _ = tag.NewKey("resource")
)

// The following metric tag keys are available from the otel-collector
// Prometheus exporter. They are created from resource attributes using the
// resource_to_telemetry_conversion feature.
var (
	// ResourceKeySyncKind groups metrics by the Sync kind. Possible values: RootSync, RepoSync.
	ResourceKeySyncKind, _ = tag.NewKey("configsync_sync_kind")

	// ResourceKeySyncName groups metrics by the Sync name.
	ResourceKeySyncName, _ = tag.NewKey("configsync_sync_name")

	// ResourceKeySyncNamespace groups metrics by the Sync namespace.
	ResourceKeySyncNamespace, _ = tag.NewKey("configsync_sync_namespace")

	// ResourceKeyDeploymentName groups metrics by k8s deployment name.
	ResourceKeyDeploymentName, _ = tag.NewKey("k8s_deployment_name")

	// ResourceKeyDeploymentName groups metrics by k8s pod name.
	ResourceKeyPodName, _ = tag.NewKey("k8s_pod_name")
)

const (
	// StatusSuccess is the string value for the status key indicating success
	StatusSuccess = "success"
	// StatusError is the string value for the status key indicating failure/errors
	StatusError = "error"
	// CommitNone is the string value for the commit key indicating that no
	// commit has been synced.
	CommitNone = "NONE"
	// ApplierController is the string value for the applier controller in the multi-repo mode
	ApplierController = "applier"
	// RemediatorController is the string value for the remediator controller in the multi-repo mode
	RemediatorController = "remediator"
)

// StatusTagKey returns a string representation of the error, if it exists, otherwise success.
func StatusTagKey(err error) string {
	if err == nil {
		return StatusSuccess
	}
	return StatusError
}

// StatusTagValueFromSummary returns error if the summary indicates at least 1
// error, otherwise success.
func StatusTagValueFromSummary(summary *v1beta1.ErrorSummary) string {
	if summary.TotalCount == 0 {
		return StatusSuccess
	}
	return StatusError
}
