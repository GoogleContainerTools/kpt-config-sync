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
	"context"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
)

var (
	keyFieldName, _              = tag.NewKey("field_name")
	keyDeprecatingField, _       = tag.NewKey("deprecating_field")
	keySimplificationAdoption, _ = tag.NewKey("simplification_field")
	keyK8sMetadata, _            = tag.NewKey("k8s_metadata_transformer")
	keyHelmMetrics, _            = tag.NewKey("helm_inflator")
	keyBaseCount, _              = tag.NewKey("base_source")
	keyPatchCount, _             = tag.NewKey("patch_field")
	keyTopTierCount, _           = tag.NewKey("top_tier_field")
)

// RecordKustomizeFieldCountData records all data relevant to the kustomization's field counts
func RecordKustomizeFieldCountData(ctx context.Context, fieldCountData *KustomizeFieldMetrics) {
	recordKustomizeFieldCount(ctx, fieldCountData.FieldCount)
	recordKustomizeDeprecatingFields(ctx, fieldCountData.DeprecationMetrics)
	recordKustomizeSimplification(ctx, fieldCountData.SimplMetrics)
	recordKustomizeK8sMetadata(ctx, fieldCountData.K8sMetadata)
	recordKustomizeHelmMetrics(ctx, fieldCountData.HelmMetrics)
	recordKustomizeBaseCount(ctx, fieldCountData.BaseCount)
	recordKustomizePatchCount(ctx, fieldCountData.PatchCount)
	recordKustomizeTopTierMetrics(ctx, fieldCountData.TopTierCount)
}

// RecordKustomizeResourceCount produces measurement for KustomizeResourceCount view
func RecordKustomizeResourceCount(ctx context.Context, resourceCount int) {
	stats.Record(ctx, KustomizeResourceCount.M(int64(resourceCount)))
}

// RecordKustomizeExecutionTime produces measurement for KustomizeExecutionTime view
func RecordKustomizeExecutionTime(ctx context.Context, executionTime float64) {
	stats.Record(ctx, KustomizeExecutionTime.M(executionTime))
}

// recordKustomizeFieldCount produces measurement for KustomizeFieldCount view
func recordKustomizeFieldCount(ctx context.Context, fieldCount map[string]int) {
	for field, count := range fieldCount {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keyFieldName, field))
		stats.Record(tagCtx, KustomizeFieldCount.M(int64(count)))
	}
}

// recordKustomizeDeprecatingFields produces measurement for KustomizeDeprecatingMetrics view
func recordKustomizeDeprecatingFields(ctx context.Context, deprecationMetrics map[string]int) {
	for field, count := range deprecationMetrics {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keyDeprecatingField, field))
		stats.Record(tagCtx, KustomizeDeprecatingFields.M(int64(count)))
	}
}

// recordKustomizeSimplification produces measurement for KustomizeSimplification view
func recordKustomizeSimplification(ctx context.Context, simplMetrics map[string]int) {
	for field, count := range simplMetrics {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keySimplificationAdoption, field))
		stats.Record(tagCtx, KustomizeSimplification.M(int64(count)))
	}
}

// recordKustomizeK8sMetadata produces measurement for KustomizeK8sMetadata view
func recordKustomizeK8sMetadata(ctx context.Context, k8sMetadata map[string]int) {
	for field, count := range k8sMetadata {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keyK8sMetadata, field))
		stats.Record(tagCtx, KustomizeK8sMetadata.M(int64(count)))
	}
}

// recordKustomizeHelmMetrics produces measurement for KustomizeHelmMetrics view
func recordKustomizeHelmMetrics(ctx context.Context, helmMetrics map[string]int) {
	for helmInflator, count := range helmMetrics {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keyHelmMetrics, helmInflator))
		stats.Record(tagCtx, KustomizeHelmMetrics.M(int64(count)))
	}
}

// recordKustomizeBaseCount produces measurement for KustomizeBaseCount view
func recordKustomizeBaseCount(ctx context.Context, baseCount map[string]int) {
	for baseSource, count := range baseCount {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keyBaseCount, baseSource))
		stats.Record(tagCtx, KustomizeBaseCount.M(int64(count)))
	}
}

// recordKustomizePatchCount produces measurement for KustomizePatchCount view
func recordKustomizePatchCount(ctx context.Context, patchCount map[string]int) {
	for patchType, count := range patchCount {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keyPatchCount, patchType))
		stats.Record(tagCtx, KustomizePatchCount.M(int64(count)))
	}
}

// recordKustomizeTopTierMetrics produces measurement for KustomizeTopTierMetrics view
func recordKustomizeTopTierMetrics(ctx context.Context, topTierCount map[string]int) {
	for field, count := range topTierCount {
		tagCtx, _ := tag.New(ctx, tag.Upsert(keyTopTierCount, field))
		stats.Record(tagCtx, KustomizeTopTierMetrics.M(int64(count)))
	}
}
