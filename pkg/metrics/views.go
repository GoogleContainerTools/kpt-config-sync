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
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

var distributionBounds = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10}

var (
	// APICallDurationView aggregates the APICallDuration metric measurements.
	APICallDurationView = &view.View{
		Name:        APICallDuration.Name(),
		Measure:     APICallDuration,
		Description: "The latency distribution of API server calls",
		TagKeys:     []tag.Key{KeyOperation, KeyStatus},
		Aggregation: view.Distribution(distributionBounds...),
	}

	// ReconcilerErrorsView aggregates the ReconcilerErrors metric measurements.
	ReconcilerErrorsView = &view.View{
		Name:        ReconcilerErrors.Name(),
		Measure:     ReconcilerErrors,
		Description: "The current number of errors in the RootSync and RepoSync reconcilers",
		TagKeys:     []tag.Key{KeyComponent, KeyErrorClass},
		Aggregation: view.LastValue(),
	}

	// PipelineErrorView aggregates the PipelineError metric measurements
	PipelineErrorView = &view.View{
		Name:        PipelineError.Name(),
		Measure:     PipelineError,
		Description: "A boolean indicates if any error happens from different stages when syncing a commit",
		TagKeys:     []tag.Key{KeyName, KeyReconcilerType, KeyComponent},
		Aggregation: view.LastValue(),
	}

	// ReconcileDurationView aggregates the ReconcileDuration metric measurements.
	ReconcileDurationView = &view.View{
		Name:        ReconcileDuration.Name(),
		Measure:     ReconcileDuration,
		Description: "The latency distribution of RootSync and RepoSync reconcile events",
		TagKeys:     []tag.Key{KeyStatus},
		Aggregation: view.Distribution(distributionBounds...),
	}

	// ParserDurationView aggregates the ParserDuration metric measurements.
	ParserDurationView = &view.View{
		Name:        ParserDuration.Name(),
		Measure:     ParserDuration,
		Description: "The latency distribution of the parse-apply-watch loop",
		TagKeys:     []tag.Key{KeyStatus, KeyTrigger, KeyParserSource},
		Aggregation: view.Distribution(distributionBounds...),
	}

	// LastSyncTimestampView aggregates the LastSyncTimestamp metric measurements.
	LastSyncTimestampView = &view.View{
		Name:        LastSync.Name(),
		Measure:     LastSync,
		Description: "The timestamp of the most recent sync from Git",
		Aggregation: view.LastValue(),
	}

	// DeclaredResourcesView aggregates the DeclaredResources metric measurements.
	DeclaredResourcesView = &view.View{
		Name:        DeclaredResources.Name(),
		Measure:     DeclaredResources,
		Description: "The current number of declared resources parsed from Git",
		Aggregation: view.LastValue(),
	}

	// ApplyOperationsView aggregates the ApplyOps metric measurements.
	ApplyOperationsView = &view.View{
		Name:        ApplyOperations.Name() + "_total",
		Measure:     ApplyOperations,
		Description: "The total number of operations that have been performed to sync resources to source of truth",
		TagKeys:     []tag.Key{KeyOperation, KeyStatus},
		Aggregation: view.Count(),
	}

	// ApplyDurationView aggregates the ApplyDuration metric measurements.
	ApplyDurationView = &view.View{
		Name:        ApplyDuration.Name(),
		Measure:     ApplyDuration,
		Description: "The latency distribution of applier resource sync events",
		TagKeys:     []tag.Key{KeyStatus},
		Aggregation: view.Distribution(distributionBounds...),
	}

	// LastApplyTimestampView aggregates the LastApplyTimestamp metric measurements.
	LastApplyTimestampView = &view.View{
		Name:        LastApply.Name(),
		Measure:     LastApply,
		Description: "The timestamp of the most recent applier resource sync event",
		TagKeys:     []tag.Key{KeyStatus, KeyCommit},
		Aggregation: view.LastValue(),
	}

	// ResourceFightsView aggregates the ResourceFights metric measurements.
	ResourceFightsView = &view.View{
		Name:        ResourceFights.Name() + "_total",
		Measure:     ResourceFights,
		Description: "The total number of resources that are being synced too frequently",
		Aggregation: view.Count(),
	}

	// RemediateDurationView aggregates the RemediateDuration metric measurements.
	RemediateDurationView = &view.View{
		Name:        RemediateDuration.Name(),
		Measure:     RemediateDuration,
		Description: "The latency distribution of remediator reconciliation events",
		TagKeys:     []tag.Key{KeyStatus},
		Aggregation: view.Distribution(distributionBounds...),
	}

	// ResourceConflictsView aggregates the ResourceConflicts metric measurements.
	ResourceConflictsView = &view.View{
		Name:        ResourceConflicts.Name() + "_total",
		Measure:     ResourceConflicts,
		Description: "The total number of resource conflicts resulting from a mismatch between the cached resources and cluster resources",
		Aggregation: view.Count(),
	}

	// InternalErrorsView aggregates the InternalErrors metric measurements.
	InternalErrorsView = &view.View{
		Name:        InternalErrors.Name() + "_total",
		Measure:     InternalErrors,
		Description: "The total number of internal errors triggered by Config Sync",
		TagKeys:     []tag.Key{KeyInternalErrorSource},
		Aggregation: view.Count(),
	}

	// RenderingCountView aggregates the RenderingCount metric measurements.
	RenderingCountView = &view.View{
		Name:        RenderingCount.Name() + "_total",
		Measure:     RenderingCount,
		Description: "The total number of renderings that are performed",
		Aggregation: view.Count(),
	}

	// SkipRenderingCountView aggregates the SkipRenderingCount metric measurements.
	SkipRenderingCountView = &view.View{
		Name:        SkipRenderingCount.Name() + "_total",
		Measure:     SkipRenderingCount,
		Description: "The total number of renderings that are skipped",
		Aggregation: view.Count(),
	}

	// ResourceOverrideCountView aggregates the ResourceOverrideCount metric measurements.
	ResourceOverrideCountView = &view.View{
		Name:        ResourceOverrideCount.Name() + "_total",
		Measure:     ResourceOverrideCount,
		Description: "The total number of RootSync/RepoSync objects including the `spec.override.resources` field",
		TagKeys:     []tag.Key{KeyContainer, KeyResourceType},
		Aggregation: view.Count(),
	}

	// GitSyncDepthOverrideCountView aggregates the GitSyncDepthOverrideCount metric measurements.
	GitSyncDepthOverrideCountView = &view.View{
		Name:        GitSyncDepthOverrideCount.Name() + "_total",
		Measure:     GitSyncDepthOverrideCount,
		Description: "The total number of RootSync/RepoSync objects including the `spec.override.gitSyncDepth` field",
		Aggregation: view.Count(),
	}

	// NoSSLVerifyCountView aggregates the NoSSLVerifyCount metric measurements.
	NoSSLVerifyCountView = &view.View{
		Name:        NoSSLVerifyCount.Name() + "_total",
		Measure:     NoSSLVerifyCount,
		Description: "The number of RootSync/RepoSync objects whose `spec.git.noSSLVerify` field is set to `true`",
		Aggregation: view.Count(),
	}
)
