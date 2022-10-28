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

package nomostest

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/pkg/errors"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/core"
)

// GetCurrentMetrics fetches metrics from the otel-collector ensuring that the
// metrics have been updated for with the most recent commit hashes.
func (nt *NT) GetCurrentMetrics(syncOptions ...MetricsSyncOption) (time.Duration, testmetrics.ConfigSyncMetrics) {
	nt.T.Helper()

	if nt.MultiRepo {
		var metrics testmetrics.ConfigSyncMetrics

		// Metrics are buffered and sent in batches to the collector.
		// So we may have to retry a few times until they're current.
		took, err := Retry(nt.DefaultWaitTimeout, func() error {
			var err error
			metrics, err = testmetrics.ParseMetrics(nt.otelCollectorPort)
			if err != nil {
				// Port forward again to fix intermittent "exit status 56" errors when
				// parsing from the port.
				nt.PortForwardOtelCollector()
				return err
			}

			for _, syncOption := range syncOptions {
				err = syncOption(&metrics)
				if err != nil {
					return err
				}
			}

			return nil
		})
		if err != nil {
			nt.T.Fatalf("unable to get latest metrics: %v", err)
		}

		nt.DebugLogMetrics(metrics)

		return took, metrics
	}

	return 0, nil
}

// DebugLogMetrics logs metrics to the debug log, if enabled
func (nt *NT) DebugLogMetrics(metrics testmetrics.ConfigSyncMetrics) {
	if !*e2e.Debug {
		return
	}
	nt.DebugLog("Logging all received metrics...")
	for name, ms := range metrics {
		for _, m := range ms {
			tagsJSONBytes, err := json.Marshal(m.TagMap())
			if err != nil {
				nt.T.Fatalf("unable to convert latest tags to json for metric %q: %v", name, err)
			}
			nt.DebugLogf("Metric received: { \"Name\": %q, \"Value\": %#v, \"Tags\": %s }", name, m.Value, string(tagsJSONBytes))
		}
	}
}

// ValidateMetrics pulls the latest metrics, updates the metrics on NT and
// executes the parameter function.
func (nt *NT) ValidateMetrics(syncOption MetricsSyncOption, fn func() error) error {
	if nt.MultiRepo {
		nt.T.Log("validating metrics...")
		var once sync.Once
		duration, err := Retry(nt.DefaultMetricsTimeout, func() error {
			duration, currentMetrics := nt.GetCurrentMetrics(syncOption)
			nt.ReconcilerMetrics = currentMetrics
			once.Do(func() {
				// Only log this once. Afterwards GetCurrentMetrics will return immediately.
				nt.T.Logf("waited %v for metrics to be current", duration)
			})
			return fn()
		})
		nt.T.Logf("waited %v for metrics to be valid", duration)
		if err != nil {
			return fmt.Errorf("validating metrics: %v", err)
		}
		return nil
	}
	return nil
}

// MetricsSyncOption determines where metrics will be synced to
type MetricsSyncOption func(csm *testmetrics.ConfigSyncMetrics) error

// SyncMetricsToLatestCommit syncs metrics to the latest synced commit
func SyncMetricsToLatestCommit(nt *NT) MetricsSyncOption {
	return func(metrics *testmetrics.ConfigSyncMetrics) error {
		for syncName := range nt.RootRepos {
			reconcilerName := core.RootReconcilerName(syncName)
			if err := metrics.ValidateMetricsCommitSynced(reconcilerName, nt.RootRepos[syncName].Hash()); err != nil {
				return err
			}
		}

		for syncNN := range nt.NonRootRepos {
			reconcilerName := core.NsReconcilerName(syncNN.Namespace, syncNN.Name)
			if err := metrics.ValidateMetricsCommitSynced(reconcilerName, nt.NonRootRepos[syncNN].Hash()); err != nil {
				return err
			}
		}

		nt.DebugLog(`Found "last_sync_timestamp" metric with commit="<latest>" and status="<any>" for all active reconcilers`)
		return nil
	}
}

// SyncMetricsToLatestCommitSyncedWithSuccess syncs metrics to the latest
// commit that was applied without errors.
func SyncMetricsToLatestCommitSyncedWithSuccess(nt *NT) MetricsSyncOption {
	return func(metrics *testmetrics.ConfigSyncMetrics) error {
		for syncName := range nt.RootRepos {
			reconcilerName := core.RootReconcilerName(syncName)
			if err := metrics.ValidateMetricsCommitSyncedWithSuccess(reconcilerName, nt.RootRepos[syncName].Hash()); err != nil {
				return err
			}
		}

		for syncNN := range nt.NonRootRepos {
			reconcilerName := core.NsReconcilerName(syncNN.Namespace, syncNN.Name)
			if err := metrics.ValidateMetricsCommitSyncedWithSuccess(reconcilerName, nt.NonRootRepos[syncNN].Hash()); err != nil {
				return err
			}
		}

		nt.DebugLog(`Found "last_sync_timestamp" metric with commit="<latest>" and status="success" for all active reconcilers`)
		return nil
	}
}

// SyncMetricsToReconcilerSourceError syncs metrics to a reconciler source error
func SyncMetricsToReconcilerSourceError(nt *NT, reconcilerName string) MetricsSyncOption {
	ntPtr := nt
	reconcilerNameCopy := reconcilerName
	return func(metrics *testmetrics.ConfigSyncMetrics) error {
		pod := ntPtr.GetDeploymentPod(reconcilerNameCopy, configmanagement.ControllerNamespace)
		err := metrics.ValidateReconcilerErrors(pod.Name, 1, 0)
		if err != nil {
			return err
		}
		ntPtr.DebugLog(`Found "reconciler_errors" metric with component="source" and value="1"`)
		return nil
	}
}

// SyncMetricsToReconcilerSyncError syncs metrics to a reconciler sync error
func SyncMetricsToReconcilerSyncError(nt *NT, reconcilerName string) MetricsSyncOption {
	ntPtr := nt
	reconcilerNameCopy := reconcilerName
	return func(metrics *testmetrics.ConfigSyncMetrics) error {
		pod := ntPtr.GetDeploymentPod(reconcilerNameCopy, configmanagement.ControllerNamespace)
		err := metrics.ValidateReconcilerErrors(pod.Name, 0, 1)
		if err != nil {
			return err
		}
		ntPtr.DebugLog(`Found "reconciler_errors" metric with component="sync" and value="1"`)
		return nil
	}
}

// ValidateMultiRepoMetrics validates all the multi-repo metrics.
// It checks all non-error metrics are recorded with the correct tags and values.
func (nt *NT) ValidateMultiRepoMetrics(reconcilerName string, numResources int, gvkMetrics ...testmetrics.GVKMetric) error {
	if nt.MultiRepo {
		// Validate metrics emitted from the reconciler-manager.
		if err := nt.ReconcilerMetrics.ValidateReconcilerManagerMetrics(); err != nil {
			return err
		}
		// Validate non-typed and non-error metrics in the given reconciler.
		if err := nt.ReconcilerMetrics.ValidateReconcilerMetrics(reconcilerName, numResources); err != nil {
			return err
		}
		// Validate metrics that have a GVK "type" TagKey.
		for _, tm := range gvkMetrics {
			if err := nt.ReconcilerMetrics.ValidateGVKMetrics(reconcilerName, tm); err != nil {
				return errors.Wrapf(err, "%s %s operation", tm.GVK, tm.APIOp)
			}
		}
	}
	return nil
}

// ValidateErrorMetricsNotFound validates that no error metrics are emitted from
// any of the reconcilers.
func (nt *NT) ValidateErrorMetricsNotFound() error {
	if nt.MultiRepo {
		for name := range nt.RootRepos {
			reconcilerName := core.RootReconcilerName(name)
			pod := nt.GetDeploymentPod(reconcilerName, configmanagement.ControllerNamespace)
			if err := nt.ReconcilerMetrics.ValidateErrorMetrics(pod.Name); err != nil {
				return err
			}
		}
		for nn := range nt.NonRootRepos {
			reconcilerName := core.NsReconcilerName(nn.Namespace, nn.Name)
			pod := nt.GetDeploymentPod(reconcilerName, configmanagement.ControllerNamespace)
			if err := nt.ReconcilerMetrics.ValidateErrorMetrics(pod.Name); err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateResourceOverrideCount validates that the `resource_override_count` metric exists
// for the correct reconciler.
func (nt *NT) ValidateResourceOverrideCount(reconcilerType, containerName, resourceType string, count int) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateResourceOverrideCount(reconcilerType, containerName, resourceType, count)
	}
	return nil
}

// ValidateResourceOverrideCountMissingTags checks that the `resource_override_count` metric misses the specific the tags.
func (nt *NT) ValidateResourceOverrideCountMissingTags(tags []metrics.Tag) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateResourceOverrideCountMissingTags(tags)
	}
	return nil
}

// ValidateGitSyncDepthOverrideCount validates the `git_sync_depth_override_count` metric.
func (nt *NT) ValidateGitSyncDepthOverrideCount(count int) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateGitSyncDepthOverrideCount(count)
	}
	return nil
}

// ValidateNoSSLVerifyCount checks that the `no_ssl_verify_count` metric has the correct value.
func (nt *NT) ValidateNoSSLVerifyCount(count int) error {
	if nt.MultiRepo {
		return nt.ReconcilerMetrics.ValidateNoSSLVerifyCount(count)
	}
	return nil
}

// ValidateMetricNotFound validates that a metric does not exist.
func (nt *NT) ValidateMetricNotFound(metricName string) error {
	if nt.MultiRepo {
		if _, ok := nt.ReconcilerMetrics[metricName]; ok {
			return errors.Errorf("Found an unexpected metric: %s", metricName)
		}
	}
	return nil
}

// ValidateReconcilerErrors validates that the `reconciler_error` metric exists
// for the correct reconciler pod and the tagged component has the correct value.
func (nt *NT) ValidateReconcilerErrors(reconcilerName string, sourceCount, syncCount int) error {
	if nt.MultiRepo {
		pod := nt.GetDeploymentPod(reconcilerName, configmanagement.ControllerNamespace)
		return nt.ReconcilerMetrics.ValidateReconcilerErrors(pod.Name, sourceCount, syncCount)
	}
	return nil
}
