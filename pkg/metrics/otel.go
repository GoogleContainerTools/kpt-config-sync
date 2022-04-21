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

	// MonitoringNamespace is the Namespace used for OpenTelemetry Collector deployment.
	MonitoringNamespace = "config-management-monitoring"

	// CollectorConfigGooglecloud is the OpenTelemetry Collector configuration with
	// the googlecloud exporter.
	CollectorConfigGooglecloud = `receivers:
  opencensus:
exporters:
  prometheus:
    endpoint: :8675
    namespace: config_sync
  googlecloud:
    metric:
      prefix: "custom.googleapis.com/opencensus/config_sync/"
      skip_create_descriptor: false
    retry_on_failure:
      enabled: false
    sending_queue:
      enabled: false
  googlecloud/kubernetes:
    metric:
      prefix: "kubernetes.io/internal/addons/config_sync/"
      skip_create_descriptor: false
    retry_on_failure:
      enabled: false
    sending_queue:
      enabled: false
processors:
  batch:
  filter/cloudmonitoring:
    metrics:
      include:
        match_type: regexp
        metric_names:
          - reconciler_errors
          - pipeline_error_observed
          - declared_resources
          - apply_operations_total
          - resource_fights_total
          - internal_errors_total
          - kcc_resource_count
          - resource_count
          - ready_resource_count
          - cluster_scoped_resource_count
          - resource_ns_count
          - api_duration_seconds
  filter/kubernetes:
    metrics:
      include:
        match_type: regexp
        metric_names:
          - kustomize.*
          - api_duration_seconds
          - reconciler_errors
          - pipeline_error_observed
          - reconcile_duration_seconds
          - parser_duration_seconds
          - declared_resources
          - apply_operations_total
          - apply_duration_seconds
          - resource_fights_total
          - remediate_duration_seconds
          - resource_conflicts_total
          - internal_errors_total
          - rendering_count_total
          - skip_rendering_count_total
          - resource_override_count_total
          - git_sync_depth_override_count_total
          - no_ssl_verify_count_total
          - kcc_resource_count
      exclude:
        match_type: strict
        metric_names:
          - rg_reconcile_duration_seconds
  metricstransform/kubernetes:
    transforms:
      - include: declared_resources
        action: update
        new_name: current_declared_resources
      - include: reconciler_errors
        action: update
        new_name: last_reconciler_errors
      - include: pipeline_error_observed
        action: update
        new_name: last_pipeline_error_observed
      - include: apply_operations_total
        action: update
        new_name: apply_operations_count
      - include: resource_fights_total
        action: update
        new_name: resource_fights_count
      - include: resource_conflicts_total
        action: update
        new_name: resource_conflicts_count
      - include: internal_errors_total
        action: update
        new_name: internal_errors_count
      - include: rendering_count_total
        action: update
        new_name: rendering_count
      - include: skip_rendering_count_total
        action: update
        new_name: skip_rendering_count
      - include: resource_override_count_total
        action: update
        new_name: resource_override_count
      - include: git_sync_depth_override_count_total
        action: update
        new_name: git_sync_depth_override_count
      - include: no_ssl_verify_count_total
        action: update
        new_name: no_ssl_verify_count
extensions:
  health_check:
service:
  extensions: [health_check]
  pipelines:
    metrics/cloudmonitoring:
      receivers: [opencensus]
      processors: [batch, filter/cloudmonitoring]
      exporters: [googlecloud]
    metrics/prometheus:
      receivers: [opencensus]
      processors: [batch]
      exporters: [prometheus]
    metrics/kubernetes:
      receivers: [opencensus]
      processors: [batch, filter/kubernetes, metricstransform/kubernetes]
      exporters: [googlecloud/kubernetes]`
)
