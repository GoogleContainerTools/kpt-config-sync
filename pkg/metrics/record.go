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
	"context"
	"os"
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/reconcilermanager"
)

// RecordAPICallDuration produces a measurement for the APICallDuration view.
func RecordAPICallDuration(ctx context.Context, operation, status string, gvk schema.GroupVersionKind, startTime time.Time) {
	tagCtx, _ := tag.New(ctx,
		tag.Upsert(KeyOperation, operation),
		//tag.Upsert(KeyType, gvk.Kind),
		tag.Upsert(KeyStatus, status))
	measurement := APICallDuration.M(time.Since(startTime).Seconds())
	stats.Record(tagCtx, measurement)
}

// RecordReconcilerErrors produces a measurement for the ReconcilerErrors view.
func RecordReconcilerErrors(ctx context.Context, component string, errs []v1beta1.ConfigSyncError) {
	class := ""
	tagCtx, _ := tag.New(ctx,
		tag.Upsert(KeyComponent, component),
		tag.Upsert(KeyErrorClass, class),
	)
	measurement := ReconcilerErrors.M(int64(len(errs)))
	stats.Record(tagCtx, measurement)
}

// RecordPipelineError produces a measurement for the PipelineError view
func RecordPipelineError(ctx context.Context, reconcilerType, component string, errLen int) {
	reconcilerName := os.Getenv(reconcilermanager.ReconcilerNameKey)
	tagCtx, _ := tag.New(ctx,
		tag.Upsert(KeyName, reconcilerName),
		tag.Upsert(KeyReconcilerType, reconcilerType),
		tag.Upsert(KeyComponent, component))
	if errLen > 0 {
		stats.Record(tagCtx, PipelineError.M(1))
	} else {
		stats.Record(tagCtx, PipelineError.M(0))
	}
}

// RecordReconcileDuration produces a measurement for the ReconcileDuration view.
func RecordReconcileDuration(ctx context.Context, status string, startTime time.Time) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyStatus, status))
	measurement := ReconcileDuration.M(time.Since(startTime).Seconds())
	stats.Record(tagCtx, measurement)
}

// RecordParserDuration produces a measurement for the ParserDuration view.
func RecordParserDuration(ctx context.Context, trigger, source, status string, startTime time.Time) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyStatus, status), tag.Upsert(KeyTrigger, trigger), tag.Upsert(KeyParserSource, source))
	measurement := ParserDuration.M(time.Since(startTime).Seconds())
	stats.Record(tagCtx, measurement)
}

// RecordLastSync produces a measurement for the LastSync view.
func RecordLastSync(ctx context.Context, commit string, timestamp time.Time) {
	measurement := LastSync.M(timestamp.Unix())
	stats.Record(ctx, measurement)
}

// RecordDeclaredResources produces a measurement for the DeclaredResources view.
func RecordDeclaredResources(ctx context.Context, numResources int) {
	measurement := DeclaredResources.M(int64(numResources))
	stats.Record(ctx, measurement)
}

// RecordApplyOperation produces a measurement for the ApplyOperations view.
func RecordApplyOperation(ctx context.Context, operation, status string, gvk schema.GroupVersionKind) {
	tagCtx, _ := tag.New(ctx,
		//tag.Upsert(KeyName, GetResourceLabels()),
		tag.Upsert(KeyOperation, operation),
		//tag.Upsert(KeyType, gvk.Kind),
		tag.Upsert(KeyStatus, status))
	measurement := ApplyOperations.M(1)
	stats.Record(tagCtx, measurement)
}

// RecordApplyDuration produces measurements for the ApplyDuration and LastApplyTimestamp views.
func RecordApplyDuration(ctx context.Context, status, commit string, startTime time.Time) {
	now := time.Now()
	lastApplyTagCtx, _ := tag.New(ctx, tag.Upsert(KeyStatus, status), tag.Upsert(KeyCommit, commit))
	tagCtx, _ := tag.New(ctx,
		tag.Upsert(KeyStatus, status),
		//tag.Upsert(KeyCommit, commit),
	)

	durationMeasurement := ApplyDuration.M(now.Sub(startTime).Seconds())
	lastApplyMeasurement := LastApply.M(now.Unix())

	stats.Record(lastApplyTagCtx, durationMeasurement, lastApplyMeasurement)
	stats.Record(tagCtx, durationMeasurement)
}

// RecordResourceFight produces measurements for the ResourceFights view.
func RecordResourceFight(ctx context.Context, operation string, gvk schema.GroupVersionKind) {
	//tagCtx, _ := tag.New(ctx,
	//tag.Upsert(KeyName, GetResourceLabels()),
	//tag.Upsert(KeyOperation, operation),
	//tag.Upsert(KeyType, gvk.Kind),
	//)
	measurement := ResourceFights.M(1)
	stats.Record(ctx, measurement)
}

// RecordRemediateDuration produces measurements for the RemediateDuration view.
func RecordRemediateDuration(ctx context.Context, status string, gvk schema.GroupVersionKind, startTime time.Time) {
	tagCtx, _ := tag.New(ctx,
		tag.Upsert(KeyStatus, status),
	//tag.Upsert(KeyType, gvk.Kind),
	)
	measurement := RemediateDuration.M(time.Since(startTime).Seconds())
	stats.Record(tagCtx, measurement)
}

// RecordResourceConflict produces measurements for the ResourceConflicts view.
func RecordResourceConflict(ctx context.Context, gvk schema.GroupVersionKind) {
	//tagCtx, _ := tag.New(ctx,
	//	tag.Upsert(KeyName, GetResourceLabels()),
	//tag.Upsert(KeyType, gvk.Kind),
	//)
	measurement := ResourceConflicts.M(1)
	stats.Record(ctx, measurement)
}

// RecordInternalError produces measurements for the InternalErrors view.
func RecordInternalError(ctx context.Context, source string) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyInternalErrorSource, source))
	measurement := InternalErrors.M(1)
	stats.Record(tagCtx, measurement)
}

// RecordRenderingCount produces measurements for the RenderingCount view.
func RecordRenderingCount(ctx context.Context) {
	//tagCtx, _ := tag.New(ctx, tag.Upsert(KeyName, GetResourceLabels()))
	measurement := RenderingCount.M(1)
	stats.Record(ctx, measurement)
}

// RecordSkipRenderingCount produces measurements for the SkipRenderingCount view.
func RecordSkipRenderingCount(ctx context.Context) {
	//tagCtx, _ := tag.New(ctx, tag.Upsert(KeyName, GetResourceLabels()))
	measurement := SkipRenderingCount.M(1)
	stats.Record(ctx, measurement)
}

// RecordResourceOverrideCount produces measurements for the ResourceOverrideCount view.
func RecordResourceOverrideCount(ctx context.Context, reconcilerType, containerName, resourceType string) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyReconcilerType, reconcilerType), tag.Upsert(KeyContainer, containerName), tag.Upsert(KeyResourceType, resourceType))
	measurement := ResourceOverrideCount.M(1)
	stats.Record(tagCtx, measurement)
}

// RecordGitSyncDepthOverrideCount produces measurements for the GitSyncDepthOverrideCount view.
func RecordGitSyncDepthOverrideCount(ctx context.Context) {
	measurement := GitSyncDepthOverrideCount.M(1)
	stats.Record(ctx, measurement)
}

// RecordNoSSLVerifyCount produces measurements for the NoSSLVerifyCount view.
func RecordNoSSLVerifyCount(ctx context.Context) {
	measurement := NoSSLVerifyCount.M(1)
	stats.Record(ctx, measurement)
}
