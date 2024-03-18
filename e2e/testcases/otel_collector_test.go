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

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	monitoringv2 "cloud.google.com/go/monitoring/apiv3/v2"
	"cloud.google.com/go/monitoring/apiv3/v2/monitoringpb"
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/api/iterator"
	"google.golang.org/genproto/googleapis/api/metric"
	"google.golang.org/genproto/googleapis/api/monitoredres"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/iam"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/workloadidentity"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metrics"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/testing/fake"
)

const (
	DefaultMonitorKSA             = "default"
	MonitorGSA                    = "e2e-test-metric-writer"
	MetricExportErrorCaption      = "One or more TimeSeries could not be written"
	UnrecognizedLabelErrorCaption = "Unrecognized metric labels"
	GCMMetricPrefix               = "custom.googleapis.com/opencensus/config_sync"
)

var GCMMetricTypes = []string{
	ocmetrics.ReconcilerErrors.Name(),
	ocmetrics.PipelineError.Name(),
	ocmetrics.ReconcileDuration.Name(),
	ocmetrics.ParserDuration.Name(),
	ocmetrics.InternalErrors.Name(),
}

// TestOtelCollectorDeployment validates that metrics reporting works for
// Google Cloud Monitoring using either workload identity or node identity.
//
// Requirements:
// - node identity:
//   - node GSA with roles/monitoring.metricWriter IAM
//
// - workload identity:
//   - e2e-test-metric-writer GSA with roles/monitoring.metricWriter IAM
//   - roles/iam.workloadIdentityUser on config-management-monitoring/default for e2e-test-metric-writer
func TestOtelCollectorDeployment(t *testing.T) {
	nt := nomostest.New(t,
		nomostesting.Reconciliation1,
		ntopts.RequireGKE(t),
		ntopts.Unstructured,
	)
	nt.T.Cleanup(func() {
		if t.Failed() {
			nt.PodLogs("config-management-monitoring", ocmetrics.OtelCollectorName, "", false)
		}
	})
	setupMetricsServiceAccount(nt)
	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "cm", ocmetrics.OtelCollectorCustomCM, "-n", configmanagement.MonitoringNamespace, "--ignore-not-found")
		nt.T.Log("Restart otel-collector pod to reset the ConfigMap and log")
		nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
		if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace); err != nil {
			nt.T.Errorf("otel-collector pod failed to come up after a restart: %v", err)
		}
	})

	nt.T.Log("Restart otel-collector pod to refresh the ConfigMap, log and IAM")
	nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace); err != nil {
		nt.T.Fatal(err)
	}

	startTime := time.Now().UTC()

	nt.T.Log("Adding test commit after otel-collector is started up so multiple commit hashes are processed in pipelines")
	namespace := fake.NamespaceObject("foo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding foo namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Watch for metrics in GCM, timeout 2 minutes")
	ctx := nt.Context
	client, err := createGCMClient(ctx)
	if err != nil {
		nt.T.Fatal(err)
	}
	// retry for 2 minutes until metric is accessible from GCM
	_, err = retry.Retry(120*time.Second, func() error {
		for _, metricType := range GCMMetricTypes {
			descriptor := fmt.Sprintf("%s/%s", GCMMetricPrefix, metricType)
			it := listMetricInGCM(ctx, nt, client, startTime, descriptor)
			return validateMetricInGCM(nt, it, descriptor, nt.ClusterName)
		}
		return nil
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Checking the otel-collector log contains no failure...")
	err = validateDeploymentLogHasNoFailure(nt, ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace, MetricExportErrorCaption)
	if err != nil {
		nt.T.Fatal(err)
	}

	// The ConfigMap that is expected to trigger duplicate time series error has
	// name 'otel-collector-custom', which by the setup in otel-collector deployment
	// will take precedence over 'otel-collector-googlecloud' that was deployed
	// by the test.
	nt.T.Log("Apply custom otel-collector ConfigMap that could cause duplicate time series error")
	nt.MustKubectl("apply", "-f", "../testdata/otel-collector/otel-cm-monarch-rejected-labels.yaml")
	nt.T.Log("Restart otel-collector pod to refresh the ConfigMap and log")
	nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Checking the otel-collector log contains failure...")
	_, err = retry.Retry(60*time.Second, func() error {
		return validateDeploymentLogHasFailure(nt, ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace, MetricExportErrorCaption)
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Remove otel-collector ConfigMap that creates duplicated time series error")
	nt.MustKubectl("delete", "cm", ocmetrics.OtelCollectorCustomCM, "-n", configmanagement.MonitoringNamespace, "--ignore-not-found")
	nt.T.Log("Restart otel-collector pod to refresh the ConfigMap, log and IAM")
	nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace); err != nil {
		nt.T.Fatal(err)
	}

	// Change the RootSync to sync from kustomize-components dir to enable Kustomize metrics
	nt.T.Log("Add the kustomize components root directory to enable kustomize metrics")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Copy("../testdata/hydration/kustomize-components", "."))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("add DRY configs to the repository"))

	nt.T.Log("Update RootSync to sync from the kustomize-components directory")
	rs := fake.RootSyncObjectV1Beta1(configsync.RootSyncName)
	nt.MustMergePatch(rs, `{"spec": {"git": {"dir": "kustomize-components"}}}`)
	syncDirMap := map[types.NamespacedName]string{
		nomostest.DefaultRootRepoNamespacedName: "kustomize-components",
	}
	if err := nt.WatchForAllSyncs(nomostest.WithSyncDirectoryMap(syncDirMap)); err != nil {
		nt.T.Fatal(err)
	}

	// retry for 2 minutes until metric is accessible from GCM
	_, err = retry.Retry(120*time.Second, func() error {
		for _, metricType := range GCMMetricTypes {
			descriptor := fmt.Sprintf("%s/%s", GCMMetricPrefix, metricType)
			it := listMetricInGCM(ctx, nt, client, startTime, descriptor)
			return validateMetricInGCM(nt, it, descriptor, nt.ClusterName)
		}
		return nil
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Checking the otel-collector log contains no failure...")
	err = validateDeploymentLogHasNoFailure(nt, ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace, MetricExportErrorCaption)
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Apply custom otel-collector ConfigMap that could cause Monarch label rejected error")
	nt.MustKubectl("apply", "-f", "../testdata/otel-collector/otel-cm-kustomize-rejected-labels.yaml")
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Checking the otel-collector log contains failure...")
	_, err = retry.Retry(60*time.Second, func() error {
		return validateDeploymentLogHasFailure(nt, ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace, UnrecognizedLabelErrorCaption)
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

// TestOtelCollectorGCMLabelAggregation validates that Google Cloud Monitoring
// metrics to ensure that the "commit" label is removed through aggregation in
// the otel-collector config.
//
// Requirements:
// - node identity:
//   - node GSA with roles/monitoring.metricWriter IAM
//
// - workload identity:
//   - e2e-test-metric-writer GSA with roles/monitoring.metricWriter IAM
//   - roles/iam.workloadIdentityUser on config-management-monitoring/default for e2e-test-metric-writer
func TestOtelCollectorGCMLabelAggregation(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.RequireGKE(t))
	setupMetricsServiceAccount(nt)

	nt.T.Log("Restarting the otel-collector pod to refresh the service account")
	nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, configmanagement.MonitoringNamespace); err != nil {
		nt.T.Fatal(err)
	}

	startTime := time.Now().UTC()

	nt.T.Log("Adding test commit after otel-collector restart")
	namespace := fake.NamespaceObject("foo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace))
	nt.Must(nt.RootRepos[configsync.RootSyncName].CommitAndPush("Adding foo namespace"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// The following metrics are sent to GCM and aggregated to remove the "commit" label.
	var metricsWithCommitLabel = []string{
		ocmetrics.LastSync.Name(),
		ocmetrics.DeclaredResources.Name(),
		ocmetrics.ApplyDuration.Name(),
		// LastApply also has commit but is filtered by filter/cloudmonitoring.
	}

	nt.T.Log("Watch for metrics in GCM, timeout 2 minutes")
	ctx := nt.Context
	client, err := createGCMClient(nt.Context)
	if err != nil {
		nt.T.Fatal(err)
	}
	// retry for 2 minutes until metric is accessible from GCM
	_, err = retry.Retry(120*time.Second, func() error {
		for _, metricType := range metricsWithCommitLabel {
			descriptor := fmt.Sprintf("%s/%s", GCMMetricPrefix, metricType)
			it := listMetricInGCM(ctx, nt, client, startTime, descriptor)
			return validateMetricInGCM(nt, it, descriptor, nt.ClusterName,
				metricDoesNotHaveLabel(metrics.KeyCommit.Name()))
		}
		return nil
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func setupMetricsServiceAccount(nt *nomostest.NT) {
	workloadPool, err := workloadidentity.GetWorkloadPool(nt)
	if err != nil {
		nt.T.Fatal(err)
	}
	// If Workload Identity enabled on cluster, setup KSA to GSA annotation.
	// Otherwise, the node identity is used.
	if workloadPool != "" {
		gsaEmail := fmt.Sprintf("%s@%s.iam.gserviceaccount.com", MonitorGSA, *e2e.GCPProject)
		if err := iam.ValidateServiceAccountExists(nt, gsaEmail); err != nil {
			nt.T.Fatal(err)
		}

		nt.T.Cleanup(func() {
			ksa := &corev1.ServiceAccount{}
			if err := nt.KubeClient.Get(DefaultMonitorKSA, configmanagement.MonitoringNamespace, ksa); err != nil {
				if apierrors.IsNotFound(err) {
					return // no need to remove annotation
				}
				nt.T.Fatalf("failed to get service account during cleanup: %v", err)
			}
			core.RemoveAnnotations(ksa, "iam.gke.io/gcp-service-account")
			if err := nt.KubeClient.Update(ksa); err != nil {
				nt.T.Fatalf("failed to remove service account annotation during cleanup: %v", err)
			}
		})

		nt.T.Log(fmt.Sprintf("Workload identity enabled, adding KSA annotation to use %s service account", MonitorGSA))
		ksa := &corev1.ServiceAccount{}
		if err := nt.KubeClient.Get(DefaultMonitorKSA, configmanagement.MonitoringNamespace, ksa); err != nil {
			nt.T.Fatalf("failed to get service account: %v", err)
		}
		core.SetAnnotation(ksa, "iam.gke.io/gcp-service-account", gsaEmail)
		if err := nt.KubeClient.Update(ksa); err != nil {
			nt.T.Fatalf("failed to set service account annotation: %v", err)
		}
		if err := nt.WatchForAllSyncs(); err != nil {
			nt.T.Fatal(err)
		}
	}
}

func validateDeploymentLogHasFailure(nt *nomostest.NT, deployment, namespace, errorString string) error {
	nt.T.Helper()

	args := []string{"logs", fmt.Sprintf("deployment/%s", deployment), "-n", namespace}
	cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
	out, err := nt.Shell.Kubectl(args...)
	if err != nil {
		nt.T.Logf("failed to run %q: %v\n%s", cmd, err, out)
		return err
	}

	entry := strings.Split(string(out), "\n")
	for _, m := range entry {
		if strings.Contains(m, errorString) {
			return nil
		}
	}
	return fmt.Errorf("error expected in the log of deployment %s, namespace %s but found none", deployment, namespace)
}

func validateDeploymentLogHasNoFailure(nt *nomostest.NT, deployment, namespace, errorString string) error {
	nt.T.Helper()

	args := []string{"logs", fmt.Sprintf("deployment/%s", deployment), "-n", namespace}
	cmd := fmt.Sprintf("kubectl %s", strings.Join(args, " "))
	out, err := nt.Shell.Kubectl(args...)
	if err != nil {
		nt.T.Logf("failed to run %q: %v\n%s", cmd, err, out)
		return err
	}

	entry := strings.Split(string(out), "\n")
	for _, m := range entry {
		if strings.Contains(m, errorString) {
			return fmt.Errorf("failure found in the log of deployment %s, namespace %s: %s", deployment, namespace, m)
		}
	}
	return nil
}

// Create a new Monitoring service client using application default credentials
func createGCMClient(ctx context.Context) (*monitoringv2.MetricClient, error) {
	client, err := monitoringv2.NewMetricClient(ctx)
	if err != nil {
		return nil, err
	}
	return client, nil
}

// Make a ListTimeSeries request of a specific metric to GCM with specified
// metricType.
// Note: metricType in this context is the metric descriptor name, for example
// "custom.googleapis.com/opencensus/config_sync/apply_operations_total".
func listMetricInGCM(ctx context.Context, nt *nomostest.NT, client *monitoringv2.MetricClient, startTime time.Time, metricType string) *monitoringv2.TimeSeriesIterator {
	endTime := time.Now().UTC()
	req := &monitoringpb.ListTimeSeriesRequest{
		Name:   "projects/" + *e2e.GCPProject,
		Filter: `metric.type="` + metricType + `" AND resource.labels.cluster_name="` + nt.ClusterName + `"`,
		Interval: &monitoringpb.TimeInterval{
			StartTime: &timestamp.Timestamp{
				Seconds: startTime.Unix(),
			},
			EndTime: &timestamp.Timestamp{
				Seconds: endTime.Unix(),
			},
		},
		View: monitoringpb.ListTimeSeriesRequest_HEADERS,
	}
	return client.ListTimeSeries(ctx, req)
}

type metricValidatorFunc func(*metric.Metric, *monitoredres.MonitoredResource) error

func metricDoesNotHaveLabel(label string) metricValidatorFunc {
	return func(_ *metric.Metric, r *monitoredres.MonitoredResource) error {
		labels := r.GetLabels()
		if value, found := labels[label]; found {
			return fmt.Errorf("expected metric to not have label, but found %s=%s", label, value)
		}
		return nil
	}
}

// Validates a metricType from a specific cluster_name can be found within given
// TimeSeries
func validateMetricInGCM(nt *nomostest.NT, it *monitoringv2.TimeSeriesIterator, metricType, clusterName string, valFns ...metricValidatorFunc) error {
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		metric := resp.GetMetric()
		resource := resp.GetResource()
		nt.Logger.Debugf(`GCM metric result: { "type": %q, "labels": %+v, "resource.type": %q, "resource.labels": %+v }`,
			metric.Type, metric.Labels, resource.Type, resource.Labels)
		if metric.GetType() == metricType {
			labels := resource.GetLabels()
			if labels["cluster_name"] == clusterName {
				for _, valFn := range valFns {
					if err := valFn(metric, resource); err != nil {
						return fmt.Errorf("GCM metric %s failed validation (cluster_name=%s): %w", metricType, nt.ClusterName, err)
					}
				}
				return nil
			}
		}
	}
	return fmt.Errorf("GCM metric %s not found (cluster_name=%s)",
		metricType, nt.ClusterName)
}
