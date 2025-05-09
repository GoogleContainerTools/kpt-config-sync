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
	"sigs.k8s.io/yaml"
)

const (
	// OpenTelemetry is the app label for all otel resources.
	OpenTelemetry = "opentelemetry"

	// OtelAgentName is the name of the OpenTelemetry Agent.
	OtelAgentName = "otel-agent"

	// OtelCollectorName is the name of the OpenTelemetry Collector.
	OtelCollectorName = "otel-collector"

	// OtelCollectorGooglecloud is the name of the OpenTelemetry Collector ConfigMap that contains Googlecloud exporter.
	OtelCollectorGooglecloud = "otel-collector-googlecloud"

	// OtelCollectorCustomCM is the name of the custom OpenTelemetry Collector ConfigMap.
	OtelCollectorCustomCM = "otel-collector-custom"
)

// CollectorConfigGooglecloudYAML returns the otel-collector-googlecloud config as YAML.
func CollectorConfigGooglecloudYAML() (string, error) {
	cfg := map[string]interface{}{
		"receivers": map[string]interface{}{
			"opencensus": map[string]interface{}{
				"endpoint": "0.0.0.0:55678",
			},
		},
		"exporters": map[string]interface{}{
			"prometheus": map[string]interface{}{
				"endpoint":  "0.0.0.0:8675",
				"namespace": "config_sync",
				"resource_to_telemetry_conversion": map[string]interface{}{
					"enabled": true,
				},
			},
			"googlecloud": map[string]interface{}{
				"metric": map[string]interface{}{
					"prefix": "custom.googleapis.com/opencensus/config_sync/",
					// The exporter would always fail at sending metric descriptor. Skipping
					// creation of metric descriptors until the error from upstream is resolved
					// The metric streaming data is not affected
					// https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/issues/529
					"skip_create_descriptor": true,
					// resource_filters looks for metric resource attributes by prefix and converts
					// them into custom metric labels, so they become visible and can be accessed
					// under the GroupBy dropdown list in Cloud Monitoring
					"resource_filters": []map[string]interface{}{
						{"prefix": "cloud.account.id"},
						{"prefix": "cloud.availability.zone"},
						{"prefix": "cloud.platform"},
						{"prefix": "cloud.provider"},
						{"prefix": "k8s.pod.ip"},
						{"prefix": "k8s.pod.namespace"},
						{"prefix": "k8s.pod.uid"},
						{"prefix": "k8s.container.name"},
						{"prefix": "host.id"},
						{"prefix": "host.name"},
						{"prefix": "k8s.deployment.name"},
						{"prefix": "k8s.node.name"},
					},
				},
				"sending_queue": map[string]interface{}{
					"enabled": false,
				},
			},
			"googlecloud/kubernetes": map[string]interface{}{
				"metric": map[string]interface{}{
					"prefix": "kubernetes.io/internal/addons/config_sync/",
					// skip_create_descriptor: Metrics start with 'kubernetes.io/' have already
					// got descriptors defined internally. Skip sending dupeicated metric
					// descriptors here to prevent errors or conflicts.
					"skip_create_descriptor": true,
					// instrumentation_library_labels: Otel Collector by default attaches
					// 'instrumentation_version' and 'instrumentation_source' labels that are
					// not specified in our Cloud Monarch definitions, thus skipping them here
					"instrumentation_library_labels": false,
					// create_service_timeseries: This is a recommended configuration for
					// 'service metrics' starts with 'kubernetes.io/' prefix. It uses
					// CreateTimeSeries API and has its own quotas, so that custom metric write
					// will not break this ingestion pipeline
					"create_service_timeseries": true,
					"service_resource_labels":   false,
				},
				"sending_queue": map[string]interface{}{
					"enabled": false,
				},
			},
		},
		"processors": map[string]interface{}{
			"batch": nil,
			// resourcedetection: This processor is needed to correctly mirror resource
			// labels from OpenCensus to OpenTelemetry. We also want to keep this same
			// processor in Otel Agent configuration as the resource labels are added from
			// there
			"resourcedetection": map[string]interface{}{
				"detectors": []string{"env", "gcp"},
			},
			"filter/cloudmonitoring": map[string]interface{}{
				"metrics": map[string]interface{}{
					"include": map[string]interface{}{
						// Use strict match type to ensure metrics like 'kustomize_resource_count'
						// is excluded
						"match_type": "strict",
						"metric_names": []string{
							"reconciler_errors",
							"apply_duration_seconds",
							"reconcile_duration_seconds",
							"rg_reconcile_duration_seconds",
							"last_sync_timestamp",
							"pipeline_error_observed",
							"declared_resources",
							"apply_operations_total",
							"resource_fights_total",
							"internal_errors_total",
							"kcc_resource_count",
							"resource_count",
							"ready_resource_count",
							"cluster_scoped_resource_count",
							"resource_ns_count",
							"api_duration_seconds",
						},
					},
				},
			},
			// Aggregate some metrics sent to Cloud Monitoring to remove high-cardinality labels (e.g. "commit")
			"metricstransform/cloudmonitoring": map[string]interface{}{
				"transforms": []map[string]interface{}{
					{"include": "last_sync_timestamp", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"configsync.sync.kind", "configsync.sync.name", "configsync.sync.namespace", "status"}, "aggregation_type": "max"}}},
					{"include": "declared_resources", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"configsync.sync.kind", "configsync.sync.name", "configsync.sync.namespace"}, "aggregation_type": "max"}}},
					{"include": "apply_duration_seconds", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"configsync.sync.kind", "configsync.sync.name", "configsync.sync.namespace", "status"}, "aggregation_type": "max"}}},
				},
			},
			"filter/kubernetes": map[string]interface{}{
				"metrics": map[string]interface{}{
					"include": map[string]interface{}{
						"match_type": "regexp",
						"metric_names": []string{
							"kustomize.*",
							"api_duration_seconds",
							"reconciler_errors",
							"pipeline_error_observed",
							"reconcile_duration_seconds",
							"rg_reconcile_duration_seconds",
							"parser_duration_seconds",
							"declared_resources",
							"apply_operations_total",
							"apply_duration_seconds",
							"resource_fights_total",
							"remediate_duration_seconds",
							"resource_conflicts_total",
							"internal_errors_total",
							"kcc_resource_count",
							"last_sync_timestamp",
						},
					},
				},
			},
			// Transform the metrics so that their names and labels are aligned with definition in go/config-sync-monarch-metrics
			"metricstransform/kubernetes": map[string]interface{}{
				"transforms": []map[string]interface{}{
					{"include": "api_duration_seconds", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"status", "operation"}, "aggregation_type": "max"}}},
					{"include": "declared_resources", "action": "update", "new_name": "current_declared_resources", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{}, "aggregation_type": "max"}}},
					{"include": "kcc_resource_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"resourcegroup"}, "aggregation_type": "max"}}},
					{"include": "reconciler_errors", "action": "update", "new_name": "last_reconciler_errors", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"component", "errorclass"}, "aggregation_type": "max"}}},
					{"include": "reconcile_duration_seconds", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"status"}, "aggregation_type": "max"}}},
					{"include": "rg_reconcile_duration_seconds", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"stallreason"}, "aggregation_type": "max"}}},
					{"include": "last_sync_timestamp", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"status"}, "aggregation_type": "max"}}},
					{"include": "parser_duration_seconds", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"status", "source", "trigger"}, "aggregation_type": "max"}}},
					{"include": "pipeline_error_observed", "action": "update", "new_name": "last_pipeline_error_observed", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"name", "component", "reconciler"}, "aggregation_type": "max"}}},
					{"include": "apply_operations_total", "action": "update", "new_name": "apply_operations_count", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"controller", "operation", "status"}, "aggregation_type": "max"}}},
					{"include": "apply_duration_seconds", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"status"}, "aggregation_type": "max"}}},
					{"include": "resource_fights_total", "action": "update", "new_name": "resource_fights_count", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"name", "component", "reconciler"}, "aggregation_type": "max"}}},
					{"include": "resource_conflicts_total", "action": "update", "new_name": "resource_conflicts_count", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{}, "aggregation_type": "max"}}},
					{"include": "internal_errors_total", "action": "update", "new_name": "internal_errors_count", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{}, "aggregation_type": "max"}}},
					{"include": "remediate_duration_seconds", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"status"}, "aggregation_type": "max"}}},
					{"include": "kustomize_field_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"field_name"}, "aggregation_type": "max"}}},
					{"include": "kustomize_deprecating_field_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"deprecating_field"}, "aggregation_type": "max"}}},
					{"include": "kustomize_simplification_adoption_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"simplification_field"}, "aggregation_type": "max"}}},
					{"include": "kustomize_builtin_transformers", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"k8s_metadata_transformer"}, "aggregation_type": "max"}}},
					{"include": "kustomize_helm_inflator_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"helm_inflator"}, "aggregation_type": "max"}}},
					{"include": "kustomize_base_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"base_source"}, "aggregation_type": "max"}}},
					{"include": "kustomize_patch_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"patch_field"}, "aggregation_type": "max"}}},
					{"include": "kustomize_ordered_top_tier_metrics", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{"top_tier_field"}, "aggregation_type": "max"}}},
					{"include": "kustomize_resource_count", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{}, "aggregation_type": "max"}}},
					{"include": "kustomize_build_latency", "action": "update", "operations": []map[string]interface{}{{"action": "aggregate_labels", "label_set": []string{}, "aggregation_type": "max"}}},
				},
			},
		},
		"extensions": map[string]interface{}{
			"health_check": map[string]interface{}{
				"endpoint": "0.0.0.0:13133",
			},
		},
		"service": map[string]interface{}{
			"extensions": []string{"health_check"},
			"pipelines": map[string]interface{}{
				// metrics/cloudmonitoring: pipeline for Cloud Monitoring backend
				"metrics/cloudmonitoring": map[string]interface{}{
					"receivers":  []string{"opencensus"},
					"processors": []string{"batch", "filter/cloudmonitoring", "metricstransform/cloudmonitoring", "resourcedetection"},
					"exporters":  []string{"googlecloud"},
				},
				// metrics/prometheus: pipeline for Prometheus backend
				"metrics/prometheus": map[string]interface{}{
					"receivers":  []string{"opencensus"},
					"processors": []string{"batch"},
					"exporters":  []string{"prometheus"},
				},
				// metrics/kubernetes: pipeline for Cloud Monarch backend
				"metrics/kubernetes": map[string]interface{}{
					"receivers":  []string{"opencensus"},
					"processors": []string{"batch", "filter/kubernetes", "metricstransform/kubernetes", "resourcedetection"},
					"exporters":  []string{"googlecloud/kubernetes"},
				},
			},
		},
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
