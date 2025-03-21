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
	"time"

	"go.opencensus.io/stats"
	"go.opencensus.io/tag"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
)

// RecordReconcileDuration produces a measurement for the ReconcileDuration view.
func RecordReconcileDuration(ctx context.Context, stallStatus string, startTime time.Time) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyStallReason, stallStatus))
	measurement := ReconcileDuration.M(time.Since(startTime).Seconds())
	stats.Record(tagCtx, measurement)
}

// RecordReadyResourceCount produces a measurement for the ReadyResourceCount view.
func RecordReadyResourceCount(ctx context.Context, nn types.NamespacedName, count int64) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyResourceGroup, nn.String()))
	measurement := ReadyResourceCount.M(count)
	stats.Record(tagCtx, measurement)
}

// RecordKCCResourceCount produces a measurement for the KCCResourceCount view.
func RecordKCCResourceCount(ctx context.Context, nn types.NamespacedName, count int64) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyResourceGroup, nn.String()))
	measurement := KCCResourceCount.M(count)
	stats.Record(tagCtx, measurement)
}

// RecordResourceCount produces a measurement for the ResourceCount view.
func RecordResourceCount(ctx context.Context, nn types.NamespacedName, count int64) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyResourceGroup, nn.String()))
	measurement := ResourceCount.M(count)
	stats.Record(tagCtx, measurement)
}

// RecordResourceGroupTotal produces a measurement for the ResourceGroupTotalView
func RecordResourceGroupTotal(ctx context.Context, count int64) {
	stats.Record(ctx, ResourceGroupTotal.M(count))
}

// RecordNamespaceCount produces a measurement for the NamespaceCount view.
func RecordNamespaceCount(ctx context.Context, nn types.NamespacedName, count int64) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyResourceGroup, nn.String()))
	measurement := NamespaceCount.M(count)
	stats.Record(tagCtx, measurement)
}

// RecordClusterScopedResourceCount produces a measurement for ClusterScopedResourceCount view
func RecordClusterScopedResourceCount(ctx context.Context, nn types.NamespacedName, count int64) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyResourceGroup, nn.String()))
	measurement := ClusterScopedResourceCount.M(count)
	stats.Record(tagCtx, measurement)
}

// RecordCRDCount produces a measurement for RecordCRDCount view
func RecordCRDCount(ctx context.Context, nn types.NamespacedName, count int64) {
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyResourceGroup, nn.String()))
	measurement := CRDCount.M(count)
	stats.Record(tagCtx, measurement)
}

// RecordPipelineError produces a measurement for PipelineErrorView
func RecordPipelineError(ctx context.Context, nn types.NamespacedName, component string, hasErr bool) {
	reconcilerName, reconcilerType := ComputeReconcilerNameType(nn)
	tagCtx, _ := tag.New(ctx, tag.Upsert(KeyComponent, component), tag.Upsert(KeyName, reconcilerName),
		tag.Upsert(KeyType, reconcilerType))
	var metricVal int64
	if hasErr {
		metricVal = 1
	} else {
		metricVal = 0
	}
	stats.Record(tagCtx, PipelineError.M(metricVal))
	klog.Infof("Recording %s metric at component: %s, namespace: %s, reconciler: %s, sync type: %s with value %v",
		PipelineErrorView.Name, component, nn.Namespace, reconcilerName, nn.Name, metricVal)
}

// ComputeReconcilerNameType computes the reconciler name from the ResourceGroup CR name
func ComputeReconcilerNameType(nn types.NamespacedName) (reconcilerName, reconcilerType string) {
	if nn.Namespace == configsync.ControllerNamespace {
		return core.RootReconcilerName(nn.Name), configsync.RootSyncName
	}
	return core.NsReconcilerName(nn.Namespace, nn.Name), configsync.RepoSyncName
}
