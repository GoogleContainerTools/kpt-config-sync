/*
Copyright 2021 Google LLC.

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

package kmetrics

import (
	"go.opencensus.io/stats"
)

var (
	// KustomizeFieldCount is the number of times a particular field is used
	KustomizeFieldCount = stats.Int64(
		"kustomize_field_count",
		"The number of times a particular field is used in the kustomization files",
		stats.UnitDimensionless)

	// KustomizeDeprecatingFields is the usage of fields that may become deprecated
	KustomizeDeprecatingFields = stats.Int64(
		"kustomize_deprecating_field_count",
		"The usage of fields that may become deprecated",
		stats.UnitDimensionless)

	// KustomizeSimplification is the usage of simplification transformers
	KustomizeSimplification = stats.Int64(
		"kustomize_simplification_adoption_count",
		"The usage of simplification transformers images, replicas, and replacements",
		stats.UnitDimensionless)

	// KustomizeK8sMetadata is the usage of builtin transformers
	KustomizeK8sMetadata = stats.Int64(
		"kustomize_builtin_transformers",
		"The usage of builtin transformers related to kubernetes object metadata",
		stats.UnitDimensionless)

	// KustomizeHelmMetrics is the usage of helm
	KustomizeHelmMetrics = stats.Int64(
		"kustomize_helm_inflator_count",
		"The usage of helm in kustomize, whether by the builtin fields or the custom function",
		stats.UnitDimensionless)

	// KustomizeBaseCount is the number of remote and local bases
	KustomizeBaseCount = stats.Int64(
		"kustomize_base_count",
		"The number of remote and local bases",
		stats.UnitDimensionless)

	// KustomizePatchCount is the number of patches
	KustomizePatchCount = stats.Int64(
		"kustomize_patch_count",
		"The number of patches in the fields `patches`, `patchesStrategicMerge`, and `patchesJson6902`",
		stats.UnitDimensionless)

	// KustomizeTopTierMetrics is the usage of high level metrics
	KustomizeTopTierMetrics = stats.Int64(
		"kustomize_ordered_top_tier_metrics",
		"Usage of Resources, Generators, SecretGenerator, ConfigMapGenerator, Transformers, and Validators",
		stats.UnitDimensionless)

	// KustomizeResourceCount is the number of resources outputted by `kustomize build`
	KustomizeResourceCount = stats.Int64(
		"kustomize_resource_count",
		"The number of resources outputted by `kustomize build`",
		stats.UnitDimensionless)

	// KustomizeExecutionTime is the execution time of `kustomize build`
	KustomizeExecutionTime = stats.Float64(
		"kustomize_build_latency",
		"Kustomize build latency",
		stats.UnitMilliseconds)
)
