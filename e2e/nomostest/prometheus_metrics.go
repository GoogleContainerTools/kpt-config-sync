// Copyright 2023 Google LLC
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
	"context"
	"fmt"
	"syscall"
	"time"

	"github.com/pkg/errors"
	prometheusapi "github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prometheusmodel "github.com/prometheus/common/model"
	"go.uber.org/multierr"
	"k8s.io/apimachinery/pkg/types"
	testmetrics "kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metrics"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/util/log"
)

const componentRendering = "rendering"
const componentSource = "source"
const componentSync = "sync"

// MetricsPredicate errors if the metrics do not match expectations.
type MetricsPredicate func(context.Context, prometheusv1.API) error

// ValidateMetrics connects to prometheus and retries the predicates in order
// until successful (with timeout).
func ValidateMetrics(nt *NT, predicates ...MetricsPredicate) error {
	ctx, cancel := context.WithCancel(nt.Context)
	defer cancel() // stop port-forward
	address, err := portForwardPrometheus(ctx, nt)
	if err != nil {
		return err
	}
	client, err := prometheusapi.NewClient(prometheusapi.Config{
		Address: fmt.Sprintf("http://%s", address),
	})
	if err != nil {
		return err
	}
	v1api := prometheusv1.NewAPI(client)

	nt.T.Log("[METRICS] validating prometheus metrics...")
	for i, predicate := range predicates {
		duration, err := Retry(nt.DefaultMetricsTimeout, func() error {
			err := predicate(ctx, v1api)
			if err != nil && errors.Is(err, syscall.ECONNREFUSED) {
				return NewTerminalError(errors.Wrapf(err, "port-forwarding failed waiting for metrics predicate[%d]", i))
			}
			return err
		})
		if err != nil {
			return errors.Wrapf(err, "timed out waiting for metrics predicate[%d]", i)
		}
		nt.T.Logf("[METRICS] waited %v for metrics to be valid", duration)
	}
	return nil
}

// ValidateStandardMetrics validates the set of standard metrics for the
// reconciler-manager and all registered RootSyncs (nt.RootRepos) and RepoSyncs
// (nt.NonRootRepos).
func ValidateStandardMetrics(nt *NT) error {
	err := ValidateMetrics(nt, ReconcilerManagerMetrics(nt))
	if err != nil {
		return err
	}
	for rsName := range nt.RootRepos {
		err = ValidateStandardMetricsForRootSync(nt, testmetrics.Summary{
			Sync: types.NamespacedName{
				Name:      rsName,
				Namespace: configmanagement.ControllerNamespace,
			},
		})
		if err != nil {
			return err
		}
	}
	for nn := range nt.NonRootRepos {
		err = ValidateStandardMetricsForRepoSync(nt, testmetrics.Summary{
			Sync: nn,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// ValidateStandardMetricsForRootSync validates the set of standard metrics for
// the specified RootSync.
func ValidateStandardMetricsForRootSync(nt *NT, summary testmetrics.Summary) error {
	if _, found := nt.RootRepos[summary.Sync.Name]; !found {
		return errors.Errorf("RootRepo %q not found", summary.Sync)
	}
	reconcilerName := core.RootReconcilerName(summary.Sync.Name)
	commitHash := nt.RootRepos[summary.Sync.Name].Hash()
	return ValidateStandardMetricsForSync(nt, configsync.RootSyncKind, reconcilerName, commitHash, summary)
}

// ValidateStandardMetricsForRepoSync validates the set of standard metrics for
// the specified RootSync.
func ValidateStandardMetricsForRepoSync(nt *NT, summary testmetrics.Summary) error {
	if _, found := nt.NonRootRepos[summary.Sync]; !found {
		return errors.Errorf("NonRootRepo %q not found", summary.Sync)
	}
	reconcilerName := core.NsReconcilerName(summary.Sync.Namespace, summary.Sync.Name)
	commitHash := nt.NonRootRepos[summary.Sync].Hash()
	return ValidateStandardMetricsForSync(nt, configsync.RepoSyncKind, reconcilerName, commitHash, summary)
}

// ValidateStandardMetricsForSync validates the set of standard metrics for the
// specified sync.
func ValidateStandardMetricsForSync(nt *NT, syncKind testmetrics.SyncKind, reconcilerName, commitHash string, summary testmetrics.Summary) error {
	count := summary.ObjectCount
	ops := summary.Operations
	if !summary.Absolute {
		// Add expected objects
		nt.DebugLogf("[METRICS] ExpectedObjects: %s", nt.MetricsExpectations)
		count += nt.MetricsExpectations.ExpectedObjectCount(syncKind, summary.Sync)
		ops = testmetrics.AppendOperations(ops,
			nt.MetricsExpectations.ExpectedObjectOperations(syncKind, summary.Sync)...)
	}
	pod, err := nt.GetDeploymentPod(reconcilerName, configmanagement.ControllerNamespace)
	if err != nil {
		return err
	}
	reconcilerPodName := pod.Name
	return ValidateMetrics(nt,
		ReconcilerSyncSuccess(nt, reconcilerPodName, commitHash),
		ReconcilerSourceMetrics(nt, reconcilerPodName, commitHash, count),
		ReconcilerSyncMetrics(nt, reconcilerPodName, commitHash),
		ReconcilerOperationsMetrics(nt, reconcilerPodName, ops...),
		ReconcilerErrorMetrics(nt, reconcilerPodName, commitHash, summary.Errors))
}

// ReconcilerManagerMetrics returns a MetricsPredicate that validates the
// ReconcileDurationView metric.
func ReconcilerManagerMetrics(nt *NT) MetricsPredicate {
	nt.DebugLogf("[METRICS] Expecting reconciler-manager reconciling status: %s", metrics.StatusSuccess)
	return func(ctx context.Context, v1api prometheusv1.API) error {
		metricName := ocmetrics.ReconcileDurationView.Name
		// ReconcileDurationView is a distribution. Query count to aggregate.
		metricName = fmt.Sprintf("%s%s%s", prometheusConfigSyncMetricPrefix, metricName, prometheusDistributionCountSuffix)
		labels := prometheusmodel.LabelSet{
			prometheusmodel.LabelName(ocmetrics.KeyStatus.Name()): prometheusmodel.LabelValue(ocmetrics.StatusSuccess),
		}
		query := fmt.Sprintf("%s%s", metricName, labels)
		return metricExists(ctx, nt, v1api, query)
	}
}

// ReconcilerSourceMetrics returns a MetricsPredicate that validates the
// DeclaredResourcesView metric.
func ReconcilerSourceMetrics(nt *NT, reconcilerPodName, commitHash string, numResources int) MetricsPredicate {
	nt.DebugLogf("[METRICS] Expecting declared resources (commit: %s): %d", commitHash, numResources)
	return func(ctx context.Context, v1api prometheusv1.API) error {
		var err error
		err = multierr.Append(err, metricDeclaredResourcesViewHasValue(ctx, nt, v1api,
			reconcilerPodName, commitHash, numResources))
		return err
	}
}

// ReconcilerSyncMetrics returns a MetricsPredicate that validates the
// LastApplyTimestampView, ApplyDurationView, and LastSyncTimestampView metrics.
func ReconcilerSyncMetrics(nt *NT, reconcilerPodName, commitHash string) MetricsPredicate {
	nt.DebugLogf("[METRICS] Expecting last apply & sync status (commit: %s): %s", commitHash, metrics.StatusSuccess)
	return func(ctx context.Context, v1api prometheusv1.API) error {
		var err error
		err = multierr.Append(err, metricLastApplyTimestampHasStatus(ctx, nt, v1api,
			reconcilerPodName, commitHash, metrics.StatusSuccess))
		err = multierr.Append(err, metricApplyDurationViewHasStatus(ctx, nt, v1api,
			reconcilerPodName, commitHash, metrics.StatusSuccess))
		err = multierr.Append(err, metricLastSyncTimestampHasStatus(ctx, nt, v1api,
			reconcilerPodName, commitHash, metrics.StatusSuccess))
		return err
	}
}

// ReconcilerOperationsMetrics returns a MetricsPredicate that validates the
// APICallDurationView, ApplyOperationsView, and RemediateDurationView metrics.
func ReconcilerOperationsMetrics(nt *NT, reconcilerPodName string, ops ...testmetrics.ObjectOperation) MetricsPredicate {
	var predicates []MetricsPredicate
	for _, op := range ops {
		if op.Operation == testmetrics.SkipOperation {
			continue
		}
		predicates = append(predicates, reconcilerOperationMetrics(nt, reconcilerPodName, op))
	}
	nt.DebugLogf("[METRICS] Expecting operations: %s", log.AsJSON(ops))
	return func(ctx context.Context, v1api prometheusv1.API) error {
		var err error
		for _, predicate := range predicates {
			err = multierr.Append(err, predicate(ctx, v1api))
		}
		return err
	}
}

func reconcilerOperationMetrics(nt *NT, reconcilerPodName string, op testmetrics.ObjectOperation) MetricsPredicate {
	return func(ctx context.Context, v1api prometheusv1.API) error {
		var err error
		err = multierr.Append(err, metricAPICallDurationViewOperationHasStatus(ctx, nt, v1api, reconcilerPodName, string(op.Operation), op.Kind, ocmetrics.StatusSuccess))
		err = multierr.Append(err, metricApplyOperationsViewHasValueAtLeast(ctx, nt, v1api, reconcilerPodName, string(op.Operation), op.Kind, ocmetrics.StatusSuccess, op.Count))
		err = multierr.Append(err, metricRemediateDurationViewHasStatus(ctx, nt, v1api, reconcilerPodName, op.Kind, ocmetrics.StatusSuccess))
		return err
	}
}

// ReconcilerErrorMetrics returns a MetricsPredicate that validates the
// following metrics:
// - ResourceFightsView
// - ResourceConflictsView
// - InternalErrorsView
// - ReconcilerErrorsView
func ReconcilerErrorMetrics(nt *NT, reconcilerPodName, commitHash string, summary testmetrics.ErrorSummary) MetricsPredicate {
	nt.DebugLogf("[METRICS] Expecting reconciler errors: %s", log.AsJSON(summary))

	var predicates []MetricsPredicate
	// Metrics aggregated by total count
	predicates = append(predicates, metricResourceFightsHasValueAtLeast(nt, reconcilerPodName, summary.Fights))
	predicates = append(predicates, metricResourceConflictsHasValueAtLeast(nt, reconcilerPodName, commitHash, summary.Conflicts))
	predicates = append(predicates, metricInternalErrorsHasValueAtLeast(nt, reconcilerPodName, summary.Internal))
	// Metrics aggregated by last value
	predicates = append(predicates, metricReconcilerErrorsHasValue(nt, reconcilerPodName, componentRendering, summary.Rendering))
	predicates = append(predicates, metricReconcilerErrorsHasValue(nt, reconcilerPodName, componentSource, summary.Source))
	predicates = append(predicates, metricReconcilerErrorsHasValue(nt, reconcilerPodName, componentSync, summary.Sync))

	return func(ctx context.Context, v1api prometheusv1.API) error {
		var err error
		for _, predicate := range predicates {
			err = multierr.Append(err, predicate(ctx, v1api))
		}
		return err
	}
}

// ReconcilerSyncSuccess returns a MetricsPredicate that validates that the
// latest commit synced successfully for the specified reconciler and commit.
func ReconcilerSyncSuccess(nt *NT, reconcilerPodName, commitHash string) MetricsPredicate {
	nt.DebugLogf("[METRICS] Expecting last sync status (commit: %s): %s", commitHash, metrics.StatusSuccess)
	return func(ctx context.Context, v1api prometheusv1.API) error {
		return metricLastSyncTimestampHasStatus(ctx, nt, v1api,
			reconcilerPodName, commitHash, metrics.StatusSuccess)
	}
}

// ReconcilerSyncError returns a MetricsPredicate that validates that the
// latest commit sync errored for the specified reconciler and commit.
func ReconcilerSyncError(nt *NT, reconcilerPodName, commitHash string) MetricsPredicate {
	nt.DebugLogf("[METRICS] Expecting last sync status (commit: %s): %s", commitHash, metrics.StatusError)
	return func(ctx context.Context, v1api prometheusv1.API) error {
		return metricLastSyncTimestampHasStatus(ctx, nt, v1api,
			reconcilerPodName, commitHash, metrics.StatusError)
	}
}

// metricReconcilerErrorsHasValue returns a MetricsPredicate that validates that
// the latest pod for the specified reconciler has emitted a reconciler error
// metric with the specified component and quantity value.
// If the expected value is zero, the metric being not found is also acceptable.
// Expected components: "rendering", "source", or "sync".
func metricReconcilerErrorsHasValue(nt *NT, reconcilerPodName, componentName string, value int) MetricsPredicate {
	return func(ctx context.Context, v1api prometheusv1.API) error {
		metricName := ocmetrics.ReconcilerErrorsView.Name
		metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
		labels := prometheusmodel.LabelSet{
			prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):         prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
			prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()):   prometheusmodel.LabelValue(reconcilerPodName),
			prometheusmodel.LabelName(ocmetrics.KeyExportedComponent.Name()): prometheusmodel.LabelValue(componentName),
		}
		// ReconcilerErrorsView only keeps the LastValue, so we don't need to aggregate
		query := fmt.Sprintf("%s%s", metricName, labels)
		if value == 0 {
			// When there's an error, the other error metrics may not all be recorded.
			// So tolerate missing metrics when expecting a zero value.
			return metricExistsWithValueOrDoesNotExist(ctx, nt, v1api, query, float64(value))
		}
		return metricExistsWithValue(ctx, nt, v1api, query, float64(value))
	}
}

// metricResourceFightsHasValueAtLeast returns a MetricsPredicate that validates
// that ResourceFights has at least the expected value.
// If the expected value is zero, the metric must be zero or not found.
func metricResourceFightsHasValueAtLeast(nt *NT, reconcilerPodName string, value int) MetricsPredicate {
	return func(ctx context.Context, v1api prometheusv1.API) error {
		metricName := ocmetrics.ResourceFightsView.Name
		metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
		labels := prometheusmodel.LabelSet{
			prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
			prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		}
		// ResourceFightsView counts the total number of ResourceFights, so we don't need to aggregate
		query := fmt.Sprintf("%s%s", metricName, labels)
		if value == 0 {
			// Tolerate missing metrics when expecting a zero value.
			// Don't allow any value other than zero.
			return metricExistsWithValueOrDoesNotExist(ctx, nt, v1api, query, float64(value))
		}
		return metricExistsWithValueAtLeast(ctx, nt, v1api, query, float64(value))
	}
}

// metricResourceConflictsHasValueAtLeast returns a MetricsPredicate that
// validates that ResourceConflicts has at least the expected value.
// If the expected value is zero, the metric must be zero or not found.
func metricResourceConflictsHasValueAtLeast(nt *NT, reconcilerPodName string, commitHash string, value int) MetricsPredicate {
	return func(ctx context.Context, v1api prometheusv1.API) error {
		metricName := ocmetrics.ResourceConflictsView.Name
		metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
		labels := prometheusmodel.LabelSet{
			prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
			prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
			prometheusmodel.LabelName(ocmetrics.KeyCommit.Name()):          prometheusmodel.LabelValue(commitHash),
		}
		// ResourceConflictsView counts the total number of ResourceConflicts, so we don't need to aggregate
		query := fmt.Sprintf("%s%s", metricName, labels)
		if value == 0 {
			// Tolerate missing metrics when expecting a zero value.
			// Don't allow any value other than zero.
			return metricExistsWithValueOrDoesNotExist(ctx, nt, v1api, query, float64(value))
		}
		return metricExistsWithValueAtLeast(ctx, nt, v1api, query, float64(value))
	}
}

// metricInternalErrorsHasValueAtLeast returns a MetricsPredicate that validates
// that InternalErrors has at least the expected value.
// If the expected value is zero, the metric must be zero or not found.
func metricInternalErrorsHasValueAtLeast(nt *NT, reconcilerPodName string, value int) MetricsPredicate {
	return func(ctx context.Context, v1api prometheusv1.API) error {
		metricName := ocmetrics.InternalErrorsView.Name
		metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
		labels := prometheusmodel.LabelSet{
			prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
			prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		}
		// InternalErrorsView counts the total number of InternalErrors, so we don't need to aggregate
		query := fmt.Sprintf("%s%s", metricName, labels)
		if value == 0 {
			// Tolerate missing metrics when expecting a zero value.
			// Don't allow any value other than zero.
			return metricExistsWithValueOrDoesNotExist(ctx, nt, v1api, query, float64(value))
		}
		return metricExistsWithValueAtLeast(ctx, nt, v1api, query, float64(value))
	}
}

func metricLastSyncTimestampHasStatus(ctx context.Context, nt *NT, v1api prometheusv1.API, reconcilerPodName, commitHash, status string) error {
	metricName := ocmetrics.LastSyncTimestampView.Name
	metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
	labels := prometheusmodel.LabelSet{
		prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
		prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		prometheusmodel.LabelName(ocmetrics.KeyCommit.Name()):          prometheusmodel.LabelValue(commitHash),
		prometheusmodel.LabelName(ocmetrics.KeyStatus.Name()):          prometheusmodel.LabelValue(status),
	}
	// LastSyncTimestampView only keeps the LastValue, so we don't need to aggregate
	query := fmt.Sprintf("%s%s", metricName, labels)
	return metricExists(ctx, nt, v1api, query)
}

func metricLastApplyTimestampHasStatus(ctx context.Context, nt *NT, v1api prometheusv1.API, reconcilerPodName, commitHash, status string) error {
	metricName := ocmetrics.LastApplyTimestampView.Name
	metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
	labels := prometheusmodel.LabelSet{
		prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
		prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		prometheusmodel.LabelName(ocmetrics.KeyCommit.Name()):          prometheusmodel.LabelValue(commitHash),
		prometheusmodel.LabelName(ocmetrics.KeyStatus.Name()):          prometheusmodel.LabelValue(status),
	}
	// LastApplyTimestampView only keeps the LastValue, so we don't need to aggregate
	query := fmt.Sprintf("%s%s", metricName, labels)
	return metricExists(ctx, nt, v1api, query)
}

func metricApplyDurationViewHasStatus(ctx context.Context, nt *NT, v1api prometheusv1.API, reconcilerPodName, commitHash, status string) error {
	metricName := ocmetrics.ApplyDurationView.Name
	// ApplyDurationView is a distribution. Query count to aggregate.
	metricName = fmt.Sprintf("%s%s%s", prometheusConfigSyncMetricPrefix, metricName, prometheusDistributionCountSuffix)
	labels := prometheusmodel.LabelSet{
		prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
		prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		prometheusmodel.LabelName(ocmetrics.KeyCommit.Name()):          prometheusmodel.LabelValue(commitHash),
		prometheusmodel.LabelName(ocmetrics.KeyStatus.Name()):          prometheusmodel.LabelValue(status),
	}
	query := fmt.Sprintf("%s%s", metricName, labels)
	return metricExists(ctx, nt, v1api, query)
}

func metricDeclaredResourcesViewHasValue(ctx context.Context, nt *NT, v1api prometheusv1.API, reconcilerPodName, commitHash string, numResources int) error {
	metricName := ocmetrics.DeclaredResourcesView.Name
	metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
	labels := prometheusmodel.LabelSet{
		prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
		prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		prometheusmodel.LabelName(ocmetrics.KeyCommit.Name()):          prometheusmodel.LabelValue(commitHash),
	}
	// DeclaredResourcesView only keeps the LastValue, so we don't need to aggregate
	query := fmt.Sprintf("%s%s", metricName, labels)
	return metricExistsWithValue(ctx, nt, v1api, query, float64(numResources))
}

func metricAPICallDurationViewOperationHasStatus(ctx context.Context, nt *NT, v1api prometheusv1.API, reconcilerPodName, operation, kind, status string) error {
	metricName := ocmetrics.APICallDurationView.Name
	// APICallDurationView is a distribution. Query count to aggregate.
	metricName = fmt.Sprintf("%s%s%s", prometheusConfigSyncMetricPrefix, metricName, prometheusDistributionCountSuffix)
	labels := prometheusmodel.LabelSet{
		prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
		prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		prometheusmodel.LabelName(ocmetrics.KeyOperation.Name()):       prometheusmodel.LabelValue(operation),
		prometheusmodel.LabelName(ocmetrics.KeyType.Name()):            prometheusmodel.LabelValue(kind),
		prometheusmodel.LabelName(ocmetrics.KeyStatus.Name()):          prometheusmodel.LabelValue(status),
	}
	query := fmt.Sprintf("%s%s", metricName, labels)
	return metricExists(ctx, nt, v1api, query)
}

func metricApplyOperationsViewHasValueAtLeast(ctx context.Context, nt *NT, v1api prometheusv1.API, reconcilerPodName, operation, kind, status string, value int) error {
	metricName := ocmetrics.ApplyOperationsView.Name
	metricName = fmt.Sprintf("%s%s", prometheusConfigSyncMetricPrefix, metricName)
	labels := prometheusmodel.LabelSet{
		prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
		prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		prometheusmodel.LabelName(ocmetrics.KeyController.Name()):      prometheusmodel.LabelValue(ocmetrics.ApplierController),
		prometheusmodel.LabelName(ocmetrics.KeyOperation.Name()):       prometheusmodel.LabelValue(operation),
		prometheusmodel.LabelName(ocmetrics.KeyType.Name()):            prometheusmodel.LabelValue(kind),
		prometheusmodel.LabelName(ocmetrics.KeyStatus.Name()):          prometheusmodel.LabelValue(status),
	}
	// ApplyOperationsView is a count, so we don't need to aggregate
	query := fmt.Sprintf("%s%s", metricName, labels)
	return metricExistsWithValueAtLeast(ctx, nt, v1api, query, float64(value))
}

func metricRemediateDurationViewHasStatus(ctx context.Context, nt *NT, v1api prometheusv1.API, reconcilerPodName, kind, status string) error {
	metricName := ocmetrics.RemediateDurationView.Name
	// RemediateDurationView is a distribution. Query count to aggregate.
	metricName = fmt.Sprintf("%s%s%s", prometheusConfigSyncMetricPrefix, metricName, prometheusDistributionCountSuffix)
	labels := prometheusmodel.LabelSet{
		prometheusmodel.LabelName(ocmetrics.KeyComponent.Name()):       prometheusmodel.LabelValue(ocmetrics.OtelCollectorName),
		prometheusmodel.LabelName(ocmetrics.ResourceKeyPodName.Name()): prometheusmodel.LabelValue(reconcilerPodName),
		prometheusmodel.LabelName(ocmetrics.KeyType.Name()):            prometheusmodel.LabelValue(kind),
		prometheusmodel.LabelName(ocmetrics.KeyStatus.Name()):          prometheusmodel.LabelValue(status),
	}
	query := fmt.Sprintf("%s%s", metricName, labels)
	return metricExists(ctx, nt, v1api, query)
}

// metricQueryNow performs the specified query with the default timeout.
// Query is debug logged. Warnings are logged. Errors are returned.
func metricQueryNow(ctx context.Context, nt *NT, v1api prometheusv1.API, query string) (prometheusmodel.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, prometheusQueryTimeout)
	defer cancel()

	nt.DebugLogf("prometheus query: %s", query)
	response, warnings, err := v1api.Query(ctx, query, time.Now())
	if err != nil {
		return nil, err
	}
	if len(warnings) > 0 {
		nt.T.Logf("prometheus warnings: %v", warnings)
	}
	return response, nil
}

// metricResultMustExist validates that the query response includes at least one
// vector or matrix result. Response is debug logged.
// This function does not perform the query, just validates and logs it.
func metricResultMustExist(nt *NT, query string, response prometheusmodel.Value) error {
	switch result := response.(type) {
	case prometheusmodel.Vector:
		if len(result) == 0 {
			return errors.Errorf("no results from prometheus query: %s", query)
		}
		nt.DebugLogf("prometheus vector response:\n%s", result)
		return nil
	case prometheusmodel.Matrix:
		if len(result) == 0 {
			return errors.Errorf("no results from prometheus query: %s", query)
		}
		nt.DebugLogf("prometheus matrix response:\n%s", result)
		return nil
	default:
		return errors.Errorf("unsupported prometheus response: %T", response)
	}
}

func metricExists(ctx context.Context, nt *NT, v1api prometheusv1.API, query string) error {
	response, err := metricQueryNow(ctx, nt, v1api, query)
	if err != nil {
		return err
	}
	return metricResultMustExist(nt, query, response)
}

func metricExistsWithValue(ctx context.Context, nt *NT, v1api prometheusv1.API, query string, value float64) error {
	response, err := metricQueryNow(ctx, nt, v1api, query)
	if err != nil {
		return err
	}
	if err := metricResultMustExist(nt, query, response); err != nil {
		return err
	}
	switch result := response.(type) {
	case prometheusmodel.Vector:
		var values []prometheusmodel.SampleValue
		for _, sample := range result {
			if sample.Value.Equal(prometheusmodel.SampleValue(value)) {
				return nil
			}
			values = append(values, sample.Value)
		}
		return errors.Errorf("value %v not found in vector response %v for query: %s", value, values, query)
	case prometheusmodel.Matrix:
		var values []prometheusmodel.SampleValue
		for _, samples := range result {
			for _, sample := range samples.Values {
				if sample.Value.Equal(prometheusmodel.SampleValue(value)) {
					return nil
				}
				values = append(values, sample.Value)
			}
		}
		return errors.Errorf("value %v not found in matrix response %v for query: %s", value, values, query)
	default:
		return errors.Errorf("unsupported prometheus response: %T", response)
	}
}

func metricExistsWithValueAtLeast(ctx context.Context, nt *NT, v1api prometheusv1.API, query string, value float64) error {
	response, err := metricQueryNow(ctx, nt, v1api, query)
	if err != nil {
		return err
	}
	if err := metricResultMustExist(nt, query, response); err != nil {
		return err
	}
	switch result := response.(type) {
	case prometheusmodel.Vector:
		var values []prometheusmodel.SampleValue
		for _, sample := range result {
			if sample.Value >= prometheusmodel.SampleValue(value) {
				return nil
			}
			values = append(values, sample.Value)
		}
		return errors.Errorf("value %v not found in vector response %v for query: %s", value, values, query)
	case prometheusmodel.Matrix:
		var values []prometheusmodel.SampleValue
		for _, samples := range result {
			for _, sample := range samples.Values {
				if sample.Value >= prometheusmodel.SampleValue(value) {
					return nil
				}
				values = append(values, sample.Value)
			}
		}
		return errors.Errorf("value %v not found in matrix response %v for query: %s", value, values, query)
	default:
		return errors.Errorf("unsupported prometheus response: %T", response)
	}
}

func metricExistsWithValueOrDoesNotExist(ctx context.Context, nt *NT, v1api prometheusv1.API, query string, value float64) error {
	response, err := metricQueryNow(ctx, nt, v1api, query)
	if err != nil {
		return err
	}
	switch result := response.(type) {
	case prometheusmodel.Vector:
		if len(result) == 0 {
			return nil // no results
		}
		nt.DebugLogf("prometheus vector response:\n%s", result)
		var values []prometheusmodel.SampleValue
		for _, sample := range result {
			if sample.Value.Equal(prometheusmodel.SampleValue(value)) {
				return nil
			}
			values = append(values, sample.Value)
		}
		return errors.Errorf("value %v not found in vector response %v for query: %s", value, values, query)
	case prometheusmodel.Matrix:
		if len(result) == 0 {
			return nil // no results
		}
		nt.DebugLogf("prometheus matrix response:\n%s", result)
		var values []prometheusmodel.SampleValue
		for _, samples := range result {
			for _, sample := range samples.Values {
				if sample.Value.Equal(prometheusmodel.SampleValue(value)) {
					return nil
				}
				values = append(values, sample.Value)
			}
		}
		return errors.Errorf("value %v not found in matrix response %v for query: %s", value, values, query)
	default:
		return errors.Errorf("unsupported prometheus response: %T", response)
	}
}
