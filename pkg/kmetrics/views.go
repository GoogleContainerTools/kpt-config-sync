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
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var (
	// KustomizeFieldCountView is the number of times a field is used
	KustomizeFieldCountView = &view.View{
		Name:        KustomizeFieldCount.Name(),
		Measure:     KustomizeFieldCount,
		Description: "The number of times a particular field is used in the kustomization files",
		TagKeys:     []tag.Key{keyFieldName},
		Aggregation: view.LastValue(),
	}

	// KustomizeDeprecatingFieldsView is the usage of fields that may become deprecated
	KustomizeDeprecatingFieldsView = &view.View{
		Name:        KustomizeDeprecatingFields.Name(),
		Measure:     KustomizeDeprecatingFields,
		Description: "The usage of fields that may become deprecated",
		TagKeys:     []tag.Key{keyDeprecatingField},
		Aggregation: view.LastValue(),
	}

	// KustomizeSimplificationView is the usage of simplification transformers
	KustomizeSimplificationView = &view.View{
		Name:        KustomizeSimplification.Name(),
		Measure:     KustomizeSimplification,
		Description: "The usage of simplification transformers images, replicas, and replacements",
		TagKeys:     []tag.Key{keySimplificationAdoption},
		Aggregation: view.LastValue(),
	}

	// KustomizeK8sMetadataView is the usage of builtin transformers
	KustomizeK8sMetadataView = &view.View{
		Name:        KustomizeK8sMetadata.Name(),
		Measure:     KustomizeK8sMetadata,
		Description: "The usage of builtin transformers related to kubernetes object metadata",
		TagKeys:     []tag.Key{keyK8sMetadata},
		Aggregation: view.LastValue(),
	}

	// KustomizeHelmMetricsView is the usage of helm in kustomize
	KustomizeHelmMetricsView = &view.View{
		Name:        KustomizeHelmMetrics.Name(),
		Measure:     KustomizeHelmMetrics,
		Description: "The usage of helm in kustomize, whether by the builtin fields or the custom function",
		TagKeys:     []tag.Key{keyHelmMetrics},
		Aggregation: view.LastValue(),
	}

	// KustomizeBaseCountView is the number of remote and local bases
	KustomizeBaseCountView = &view.View{
		Name:        KustomizeBaseCount.Name(),
		Measure:     KustomizeBaseCount,
		Description: "The number of remote and local bases",
		TagKeys:     []tag.Key{keyBaseCount},
		Aggregation: view.LastValue(),
	}

	// KustomizePatchCountView is the number of patches
	KustomizePatchCountView = &view.View{
		Name:        KustomizePatchCount.Name(),
		Measure:     KustomizePatchCount,
		Description: "The number of patches in the fields `patches`, `patchesStrategicMerge`, and `patchesJson6902`",
		TagKeys:     []tag.Key{keyPatchCount},
		Aggregation: view.LastValue(),
	}

	// KustomizeTopTierMetricsView is the usage of high level metrics
	KustomizeTopTierMetricsView = &view.View{
		Name:        KustomizeTopTierMetrics.Name(),
		Measure:     KustomizeTopTierMetrics,
		Description: "Usage of Resources, Generators, SecretGenerator, ConfigMapGenerator, Transformers, and Validators",
		TagKeys:     []tag.Key{keyTopTierCount},
		Aggregation: view.LastValue(),
	}

	// KustomizeResourceCountView is the number of resources outputted by `kustomize build`
	KustomizeResourceCountView = &view.View{
		Name:        KustomizeResourceCount.Name(),
		Measure:     KustomizeResourceCount,
		Description: "The number of resources outputted by `kustomize build`",
		Aggregation: view.Sum(),
	}

	// KustomizeExecutionTimeView is the execution time of `kustomize build`
	KustomizeExecutionTimeView = &view.View{
		Name:        KustomizeExecutionTime.Name(),
		Measure:     KustomizeExecutionTime,
		Description: "Execution time of `kustomize build`",
		Aggregation: view.Distribution(0, 10, 20, 40, 80, 160, 320, 640, 1280, 2560, 5120, 10240),
	}
)

// RegisterKustomizeMetricsViews registers the views so that recorded metrics can be exported. .
func RegisterKustomizeMetricsViews() error {
	return view.Register(
		KustomizeFieldCountView,
		KustomizeDeprecatingFieldsView,
		KustomizeSimplificationView,
		KustomizeK8sMetadataView,
		KustomizeHelmMetricsView,
		KustomizeBaseCountView,
		KustomizePatchCountView,
		KustomizeTopTierMetricsView,
		KustomizeResourceCountView,
		KustomizeExecutionTimeView,
	)
}
