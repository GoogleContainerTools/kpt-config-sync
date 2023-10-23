/*
Copyright 2020 Google LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"go.opencensus.io/stats"
)

const namespace = "resourcegroup"

var (
	// ReconcileDuration tracks the time duration in seconds of reconciling
	// a ResourceGroup CR by the ResourceGroup controller.
	// label `reason`: the `Reason` field of the `Stalled` condition in a ResourceGroup CR.
	// reason can be: StartReconciling, FinishReconciling, ComponentFailed, ExceedTimeout.
	// This metric should be updated in the ResourceGroup controller.
	ReconcileDuration = stats.Float64(
		"rg_reconcile_duration_seconds",
		"Time duration in seconds of reconciling a ResourceGroup CR by the ResourceGroup controller",
		stats.UnitSeconds)

	// ResourceGroupTotal tracks the total number of ResourceGroup CRs in a cluster.
	// This metric should be updated in the Root controller.
	ResourceGroupTotal = stats.Int64(
		"resource_group_total",
		"Total number of ResourceGroup CRs in a cluster",
		stats.UnitDimensionless)

	// ResourceCount tracks the number of resources in a ResourceGroup CR.
	// This metric should be updated in the Root controller.
	ResourceCount = stats.Int64(
		"resource_count",
		"The number of resources in a ResourceGroup CR",
		stats.UnitDimensionless)

	// ReadyResourceCount tracks the number of resources with Current status in a ResourceGroup CR.
	// This metric should be updated in the ResourceGroup controller.
	ReadyResourceCount = stats.Int64(
		"ready_resource_count",
		"The number of resources with Current status in a ResourceGroup CR",
		stats.UnitDimensionless)

	// KCCResourceCount tracks the number of KCC resources in a ResourceGroup CR.
	// This metric should be updated in the ResourceGroup controller.
	KCCResourceCount = stats.Int64(
		"kcc_resource_count",
		"The number of KCC resources in a ResourceGroup CR",
		stats.UnitDimensionless)

	// NamespaceCount tracks the number of resource namespaces in a ResourceGroup CR.
	// This metric should be updated in the Root controller.
	NamespaceCount = stats.Int64(
		"resource_ns_count",
		"The number of resource namespaces in a ResourceGroup CR",
		stats.UnitDimensionless)

	// ClusterScopedResourceCount tracks the number of cluster-scoped resources in a ResourceGroup CR.
	// This metric should be updated in the Root controller.
	ClusterScopedResourceCount = stats.Int64(
		"cluster_scoped_resource_count",
		"The number of cluster-scoped resources in a ResourceGroup CR",
		stats.UnitDimensionless)

	// CRDCount tracks the number of CRDs in a ResourceGroup CR.
	// This metric should be updated in the Root controller.
	CRDCount = stats.Int64(
		"crd_count",
		"The number of CRDs in a ResourceGroup CR",
		stats.UnitDimensionless)

	// PipelineError tracks the error that happened when syncing a commit
	PipelineError = stats.Int64(
		"pipeline_error_observed",
		"A boolean value indicates if error happened at readiness stage when syncing a commit",
		stats.UnitDimensionless)
)
