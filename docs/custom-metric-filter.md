# ACM Custom Metric Filtering

This guide explains how to adjust the custom metrics that Config Sync
exports to Prometheus, Cloud Monitoring (formerly known as Stackdriver), and
Monarch (Google's internal metrics aggregator).

## Background

When Config Sync is installed on a GKE cluster where a
[default service account](https://cloud.google.com/kubernetes-engine/docs/tutorials/authenticating-to-cloud-platform)
is available, or on a GKE cluster that has
[Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity)
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
2. `otel-collector-googlecloud` (or `otel-collector-stackdriver` before Config Sync v1.12.0)

   Config Sync automatically deploys this configuration when Workload Identity is configured, usually on a GKE cluster. 
3. `otel-collector-custom`

    Users can apply this ConfigMap and configure their own metric pipelines.
    Use this ConfigMap instead of modifying the others in-place, to ensure modifications don't get reverted by Config Sync.

For more details on the ConfigMaps and instructions, please refer to the [monitoring doc](https://cloud.google.com/anthos-config-management/docs/how-to/monitoring-config-sync).

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
format and later can be scraped by a prometheus server following [this guide](https://cloud.google.com/anthos-config-management/docs/how-to/monitoring-config-sync#prometheus)

Here is [the current configuration](https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/pkg/metrics/otel.go#L38) of Open Telemetry Collector
in Config Sync.

## Patch the otel collector deployment with ConfigMap

1. **Get the current ConfigMap as template**

    ```
    kubectl get cm otel-collector-googlecloud -n config-management-monitoring -o yaml > otel-collector-config.yaml
    ```
    
    Or in Config Sync with version prior to `v1.11.1`, use
    
    ```
    kubectl get cm otel-collector-stackdriver -n config-management-monitoring -o yaml > otel-collector-config.yaml
    ```
1. Change the `.metadata.name` to `otel-collector-custom`

    This step spawns a new ConfigMap with name `otel-collector-custom` that is recognized by Config Sync, which overrides the `otel-collector-googlecloud` ConfigMap. Documents of the custom collector can be found [here](https://cloud.google.com/anthos-config-management/docs/how-to/monitoring-config-sync#custom-exporter). 

1. **Modify the otel-collector-config.yaml file**

    - To **add or drop metrics** from the
       [available metrics](https://cloud.google.com/anthos-config-management/docs/how-to/monitoring-config-sync#metrics),
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
