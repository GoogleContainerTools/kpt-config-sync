# ACM Custom Metric Filtering

This guide explains how to adjust the custom metrics that Config Sync
exports to Prometheus, Cloud Monitoring (formerly known as Stackdriver), and
Monarch (Google's internal metrics aggregator).

## Background

When Config Sync is installed on a GKE cluster where a
[default service account](http://cloud/kubernetes-engine/docs/tutorials/authenticating-to-cloud-platform)
is available, or on a GKE cluster that has
[Workload Identity](http://cloud/kubernetes-engine/docs/how-to/workload-identity)
enabled and
[IAM is correctly setup](https://cloud.google.com/anthos-config-management/docs/how-to/monitoring-config-sync#custom-monitoring),
Config Sync will try to apply the Open Telemetry Collector ConfigMap that contains
[googlecloud exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/googlecloudexporter)
to write metrics into Cloud Monitoring. The default configuration contains a
bare minimum list of metrics, but some customers might want to configure
different backends or fine tune the metric list for better monitoring solutions.

The `otel-collector` deployment in Config Sync accepts three types of ConfigMap
when configuring pipelines:

1. `otel-collector`

   This is the default configuration that Config Sync deploys in a non-GKE environment, which only exports the metrics through Prometheus exporter
1. `otel-collector-googlecloud` (or `otel-collector-stackdriver` before Config Sync v1.12.0)

   Config Sync automatically deploys this configuration when Workload Identity is configured, usually on a GKE cluster. 
1. `otel-collector-custom`

    Users can apply this ConfigMap and configure their own metric pipelines.
    Use this ConfigMap instead of modifying the others in-place, to ensure modifications don't get reverted by Config Sync.

For more details on the ConfigMaps and instructions, please refer to the [monitoring doc](http://cloud/anthos-config-management/docs/how-to/monitoring-config-sync).

This guide includes instructions for how to adjust filters, modify exporters, 
or opt out of exporting metrics all together.

Note: This workaround applies to Config Sync v1.8+, when custom monitoring
was integrated.

## Default metrics pipeline config (v1.12+)

Config Sync will check for the default credential in the environment when
starting up. For a GKE cluster which always have a default service account,
Config Sync will deploy the following Open Telemetry Collector configuration to
enable the Google Cloud monitoring pipelines.

A pipeline consist of:

-   receivers: receives metrics from all reconcilers inside Config Sync
-   processors: filters or transforms metrics before sending out
-   exporters: various backend endpoints and their configurations

Config Sync uses
[filter processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/filterprocessor)
to control the exclusive list of metrics that goes into each monitoring backend.

Config Sync uses two major exporters: [googlecloud exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/googlecloudexporter)
[prometheus exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/prometheusexporter)

- `googlecloud` stands for the configuration that exports to the Cloud
Monitoring backend.
- `googlecloud/kubernetes` stands for the configuration that exports to the
Cloud Monarch backend
- `prometheus` stands for the configuration that exports metrics in Prometheus
format and later can be scraped by a prometheus server following [this guide](http://cloud/anthos-config-management/docs/how-to/monitoring-config-sync#prometheus)

Here is [the current configuration](https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/pkg/metrics/otel.go#L38) of Open Telemetry Collector
in Config Sync.

## Patch the otel collector deployment with ConfigMap

1. **Get the current ConfigMap as template**

    If you are using Config Sync prior to `1.14.0`, run    

    ```
    kubectl get cm otel-collector-googlecloud -n config-management-monitoring -o yaml > otel-collector-config.yaml
    ```
    
    Or in Config Sync with version prior to `v1.11.1`, run
    
    ```
    kubectl get cm otel-collector-stackdriver -n config-management-monitoring -o yaml > otel-collector-config.yaml
    ```
   
    Or if you installed ACM 1.14.0, use the [sample Configmap](#sample-configmap) that Config Sync applies
    when [Kubernetes default service account](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#use-the-default-service-account-to-access-the-api-server)
    exists, to avoid the formatting issue when accessing the ConfigMap.

1. Change the `.metadata.name` to `otel-collector-custom`

    This step spawns a new ConfigMap with name `otel-collector-custom` that is recognized by Config Sync, which overrides the `otel-collector-googlecloud` ConfigMap. Documents of the custom collector can be found [here](http://cloud/anthos-config-management/docs/how-to/monitoring-config-sync#custom-exporter). 

1. **Modify the otel-collector-config.yaml file**

    - To **add or drop metrics** from the
       [available metrics](http://cloud/anthos-config-management/docs/how-to/monitoring-config-sync#metrics),
       modify the `include` section under the
       [filter processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/filterprocessor)
       configuration

       > Note: If running Config Sycn version prior to 1.11.x, use the
       [default config](https://github.com/tiffanny29631/kpt-config-sync/blob/main/pkg/metrics/otel.go#L38)

       ```
       # This is an example adding crd_count metric
       # Make sure you are modifying the `filter/cloudmonitoring`
       ...
       processors:
         ...
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
                 - crd_count         <----- Here
       ```

    - To **turn off the custom metrics** in Cloud Monitoring, remove this section

      > The stackdriver exporter was renamed to googlecloud after otel-collector was upgraded in Config Sync 1.12.0.

        ```
        metrics/cloudmonitoring:
          receivers: [opencensus]
          processors: [batch, filter/cloudmonitoring]
          exporters: [googlecloud]
        ```

    - To **turn off the report to Cloud Monarch**, remove this section

         > Note: In Config Sync version prior to 1.12.0, the exporter name was `stackdriver`
         instead of `googlecloud`. This is not supported when using ACM.

         ```
         metrics/kubernetes:
           receivers: [opencensus]
           processors: [batch, filter/kubernetes, metricstransform/kubernetes]
           exporters: [googlecloud/kubernetes]
         ```

1. Apply the config map

    ```
    kubectl apply -f otel-collector-config.yaml
    ```

1. Restart the Otel Collector Pod to pick up the new version of ConfigMap.
This can be done by deleting the Otel Collector deployment.

    ```
    kubectl delete deployment/otel-collector -n config-management-monitoring
    ```

1. Check everything is up an running
    
    - `kubectl get all -n config-management-monitoring` should show all objects running with no error.
    - `kubectl get cm -n config-management-monitoring` should include a new ConfigMap named `otel-collector-custom`

## Sample Configmap

```yaml
# sample-otel-collector-custom.yaml
apiVersion: v1
data:
  otel-collector-config.yaml: |-
    receivers:
      opencensus:
    exporters:
      prometheus:
        endpoint: :8675
        namespace: config_sync
      googlecloud:
        metric:
          prefix: "custom.googleapis.com/opencensus/config_sync/"
          skip_create_descriptor: true
          instrumentation_library_labels: false
        retry_on_failure:
          enabled: false
        sending_queue:
          enabled: false
      googlecloud/kubernetes:
        metric:
          prefix: "kubernetes.io/internal/addons/config_sync/"
          skip_create_descriptor: true
          instrumentation_library_labels: false
          service_resource_labels: false
        retry_on_failure:
          enabled: false
        sending_queue:
          enabled: false
    processors:
      batch:
      resourcedetection:
        detectors: [env, gcp]
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
              - kcc_resource_count_total
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
          processors: [resourcedetection, batch, filter/cloudmonitoring]
          exporters: [googlecloud]
        metrics/prometheus:
          receivers: [opencensus]
          processors: [batch]
          exporters: [prometheus]
        metrics/kubernetes:
          receivers: [opencensus]
          processors: [resourcedetection, batch, filter/kubernetes, metricstransform/kubernetes]
          exporters: [googlecloud/kubernetes]
kind: ConfigMap
metadata:
  labels:
    app: opentelemetry
    component: otel-collector
    configmanagement.gke.io/arch: csmr
    configmanagement.gke.io/system: "true"
  name: otel-collector-custom
  namespace: config-management-monitoring
```
