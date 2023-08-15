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
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/retry"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/kinds"
	ocmetrics "kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DefaultMonitorKSA     = "default"
	MonitorGSA            = "e2e-test-metric-writer"
	GCMExportErrorCaption = "failed to export time series to GCM"
	GCMMetricPrefix       = "custom.googleapis.com/opencensus/config_sync"
)

var GCMMetricTypes = []string{ocmetrics.ReconcilerErrors.Name(), ocmetrics.PipelineError.Name(), ocmetrics.ReconcileDuration.Name(), ocmetrics.ParserDuration.Name(), ocmetrics.InternalErrors.Name()}

func TestOtelCollectorDeployment(t *testing.T) {
	nt := nomostest.New(t, nomostesting.Reconciliation1, ntopts.RequireGKE(t))
	nt.T.Cleanup(func() {
		if t.Failed() {
			nt.PodLogs("config-management-monitoring", ocmetrics.OtelCollectorName, "", false)
		}
	})

	nt.T.Cleanup(func() {
		nt.MustKubectl("delete", "-f", "../testdata/otel-collector/otel-cm-duplicate-timeseries.yaml", "--ignore-not-found")
		nt.T.Log("Restart otel-collector pod to reset the ConfigMap and log")
		nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
		if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, ocmetrics.MonitoringNamespace); err != nil {
			nt.T.Errorf("otel-collector pod failed to come up after a restart: %v", err)
		}
		ksa := fake.ServiceAccountObject(DefaultMonitorKSA)
		if err := nt.KubeClient.Get(DefaultMonitorKSA, ocmetrics.MonitoringNamespace, ksa); err != nil {
			nt.T.Errorf("service account not found during cleanup: %v", err)
		}
		_, err := unsetAnnotation(nt.KubeClient, "iam.gke.io/gcp-service-account", ksa)
		if err != nil {
			nt.T.Errorf("failed to deannotate service account during cleanup: %v", err)
		}
	})

	// If Workload Identity enabled on cluster, setup KSA to GSA annotation
	if workloadPool, err := getWorkloadPool(nt); err != nil {
		nt.T.Fatal(err)
	} else if workloadPool != "" {
		nt.T.Log(fmt.Sprintf("Workload identity enabled, adding KSA annotation to use %s service account", MonitorGSA))
		ksa := fake.ServiceAccountObject(DefaultMonitorKSA)
		if err := nt.KubeClient.Get(DefaultMonitorKSA, ocmetrics.MonitoringNamespace, ksa); err != nil {
			nt.T.Fatal(fmt.Sprintf("service account does not exist: %v", err))
		}
		if _, err := setAnnotation(nt.KubeClient, "iam.gke.io/gcp-service-account",
			fmt.Sprintf("%s@%s.iam.gserviceaccount.com", MonitorGSA, *e2e.GCPProject), ksa); err != nil {
			nt.T.Fatal(err)
		}
	}

	nt.T.Log("Restart otel-collector pod to refresh the ConfigMap, log and IAM")
	nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, ocmetrics.MonitoringNamespace); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Adding test commit after otel-collector is started up so multiple commit hashes are processed in pipelines")
	namespace := fake.NamespaceObject("foo")
	nt.Must(nt.RootRepos[configsync.RootSyncName].Add("acme/ns.yaml", namespace))

	nt.T.Log("Watch for metrics in GCM, timeout 2 minutes")
	ctx := context.Background()
	client, err := createGCMClient(ctx)
	if err != nil {
		nt.T.Fatal(err)
	}
	startTime := time.Now().UTC()
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
	err = validateDeploymentLogHasNoFailure(nt, ocmetrics.OtelCollectorName, ocmetrics.MonitoringNamespace, GCMExportErrorCaption)
	if err != nil {
		nt.T.Fatal(err)
	}

	// The ConfigMap that is expected to trigger duplicate time series error has
	// name 'otel-collector-custom', which by the setup in otel-collector deployment
	// will take precedence over 'otel-collector-googlecloud' that was deployed
	// by the test.
	nt.T.Log("Apply custom otel-collector ConfigMap that could cause duplicate time series error")
	nt.MustKubectl("apply", "-f", "../testdata/otel-collector/otel-cm-duplicate-timeseries.yaml")
	nt.T.Log("Restart otel-collector pod to refresh the ConfigMap and log")
	nomostest.DeletePodByLabel(nt, "app", ocmetrics.OpenTelemetry, false)
	if err := nt.Watcher.WatchForCurrentStatus(kinds.Deployment(), ocmetrics.OtelCollectorName, ocmetrics.MonitoringNamespace); err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Checking the otel-collector log contains failure...")
	_, err = retry.Retry(60*time.Second, func() error {
		return validateDeploymentLogHasFailure(nt, ocmetrics.OtelCollectorName, ocmetrics.MonitoringNamespace, GCMExportErrorCaption)
	})
	if err != nil {
		nt.T.Fatal(err)
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

// Validates a metricType from a specific cluster_name can be found within given
// TimeSeries
func validateMetricInGCM(nt *nomostest.NT, it *monitoringv2.TimeSeriesIterator, metricType, clusterName string) error {
	for {
		resp, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return err
		}
		if resp.GetMetric().GetType() == metricType &&
			resp.GetResource().GetLabels()["cluster_name"] == clusterName {
			return nil
		}
	}
	return fmt.Errorf("did not find target time series in GCM: type %s, cluster name %s", metricType, nt.ClusterName)
}

func setAnnotation(client *testkubeclient.KubeClient, annotationKey, annotationValue string, obj client.Object) (bool, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	if annotations[annotationKey] == annotationValue {
		return false, nil
	}
	annotations[annotationKey] = annotationValue
	obj.SetAnnotations(annotations)
	if err := client.Update(obj); err != nil {
		return false, fmt.Errorf("failed to apply updated object to cluster: %v", err)
	}
	return true, nil
}

func unsetAnnotation(client *testkubeclient.KubeClient, annotationKey string, obj client.Object) (bool, error) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false, nil
	}
	if _, exists := annotations[annotationKey]; !exists {
		return false, nil
	}
	delete(annotations, annotationKey)
	obj.SetAnnotations(annotations)
	if err := client.Update(obj); err != nil {
		return false, fmt.Errorf("failed to apply updated object to cluster: %v", err)
	}
	return true, nil
}
