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

import "go.opencensus.io/stats"

const (
	// APICallDurationName is the name of API duration metric
	APICallDurationName = "api_duration_seconds"
	// ReconcilerErrorsName is the name of reconciler error count metric
	ReconcilerErrorsName = "reconciler_errors"
	// PipelineErrorName is the name of pipeline error status metric.
	PipelineErrorName = "pipeline_error_observed"
	// ReconcileDurationName is the name of reconcile duration metric
	ReconcileDurationName = "reconcile_duration_seconds"
	// ParserDurationName is the name of parser duration metric
	ParserDurationName = "parser_duration_seconds"
	// LastSyncName is the name of last sync timestamp metric
	LastSyncName = "last_sync_timestamp"
	// DeclaredResourcesName is the name of declared resource count metric
	DeclaredResourcesName = "declared_resources"
	// ApplyOperationsName is the name of apply operations count metric
	ApplyOperationsName = "apply_operations_total"
	// ApplyDurationName is the name of apply duration metric
	ApplyDurationName = "apply_duration_seconds"
	// ResourceFightsName is the name of resource fight count metric
	ResourceFightsName = "resource_fights_total"
	// RemediateDurationName is the name of remediate duration metric
	RemediateDurationName = "remediate_duration_seconds"
	// LastApplyName is the name of last apply timestamp metric
	LastApplyName = "last_apply_timestamp"
	// ResourceConflictsName is the name of resource conflict count metric
	ResourceConflictsName = "resource_conflicts_total"
	// InternalErrorsName is the name of internal error count metric
	InternalErrorsName = "internal_errors_total"
)

var (
	// APICallDuration metric measures the latency of API server calls.
	APICallDuration = stats.Float64(
		APICallDurationName,
		"The duration of API server calls in seconds",
		stats.UnitSeconds)

	// ReconcilerErrors metric measures the number of errors in the reconciler.
	ReconcilerErrors = stats.Int64(
		ReconcilerErrorsName,
		"The number of errors in the reconciler",
		stats.UnitDimensionless)

	// PipelineError metric measures the error by components when syncing a commit.
	// Definition here must exactly match the definition in the resource-group
	// controller, or the Prometheus exporter will error. b/247516388
	// https://github.com/GoogleContainerTools/kpt-resource-group/blob/main/controllers/metrics/metrics.go#L88
	PipelineError = stats.Int64(
		PipelineErrorName,
		"A boolean value indicates if error happened at readiness stage when syncing a commit",
		stats.UnitDimensionless)

	// ReconcileDuration metric measures the latency of reconcile events.
	ReconcileDuration = stats.Float64(
		ReconcileDurationName,
		"The duration of reconcile events in seconds",
		stats.UnitSeconds)

	// ParserDuration metric measures the latency of the parse-apply-watch loop.
	ParserDuration = stats.Float64(
		ParserDurationName,
		"The duration of the parse-apply-watch loop in seconds",
		stats.UnitSeconds)

	// LastSync metric measures the timestamp of the latest Git sync.
	LastSync = stats.Int64(
		LastSyncName,
		"The timestamp of the most recent sync from Git",
		stats.UnitDimensionless)

	// DeclaredResources metric measures the number of declared resources parsed from Git.
	DeclaredResources = stats.Int64(
		DeclaredResourcesName,
		"The number of declared resources parsed from Git",
		stats.UnitDimensionless)

	// ApplyOperations metric measures the number of applier apply events.
	ApplyOperations = stats.Int64(
		ApplyOperationsName,
		"The number of operations that have been performed to sync resources to source of truth",
		stats.UnitDimensionless)

	// ApplyDuration metric measures the latency of applier apply events.
	ApplyDuration = stats.Float64(
		ApplyDurationName,
		"The duration of applier events in seconds",
		stats.UnitSeconds)

	// ResourceFights metric measures the number of resource fights.
	ResourceFights = stats.Int64(
		ResourceFightsName,
		"The number of resources that are being synced too frequently",
		stats.UnitDimensionless)

	// RemediateDuration metric measures the latency of remediator reconciliation events.
	RemediateDuration = stats.Float64(
		RemediateDurationName,
		"The duration of remediator reconciliation events",
		stats.UnitSeconds)

	// LastApply metric measures the timestamp of the most recent applier apply event.
	LastApply = stats.Int64(
		LastApplyName,
		"The timestamp of the most recent applier event",
		stats.UnitDimensionless)

	// ResourceConflicts metric measures the number of resource conflicts.
	ResourceConflicts = stats.Int64(
		ResourceConflictsName,
		"The number of resource conflicts resulting from a mismatch between the cached resources and cluster resources",
		stats.UnitDimensionless)

	// InternalErrors metric measures the number of unexpected internal errors triggered by defensive checks in Config Sync.
	InternalErrors = stats.Int64(
		InternalErrorsName,
		"The number of internal errors triggered by Config Sync",
		stats.UnitDimensionless)
)
