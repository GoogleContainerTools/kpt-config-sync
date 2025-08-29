# Config Sync Custom Monitoring

This guide explains how to adjust the custom metrics that Config Sync
exports to Prometheus, Cloud Monitoring (formerly known as Stackdriver), and Cloud
Monarch (Google's internal metrics aggregator).

## ⚠️ Important Warning

**This solution is provided for convenience and is not maintained by Config Sync.**

Custom configurations override the default settings and persist between upgrades. Config Sync tries to maintain the consistency of metrics. When you upgrade Config Sync to a new version, if you created a `otel-collector-custom` ConfigMap for a previous version, your custom settings might not be compatible with the new version of Config Sync. For example, metrics names, labels, and attributes can change between Config Sync versions.

When changes are made to Config Sync metrics, they are announced in the [release notes](https://cloud.google.com/kubernetes-engine/enterprise/config-sync/docs/release-notes). These changes might require you to update your custom otel-collector configuration.

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
2. `otel-collector-googlecloud`

   Config Sync automatically deploys this configuration when Workload Identity is configured, usually on a GKE cluster. 
3. `otel-collector-custom`

    Users can apply this ConfigMap and configure their own metric pipelines.
    Use this ConfigMap instead of modifying the others in-place, to ensure modifications don't get reverted by Config Sync.

For more details on the ConfigMaps and instructions, please refer to the [monitoring doc](http://cloud/anthos-config-management/docs/how-to/monitoring-config-sync).

This guide includes instructions for how to adjust filters, modify exporters, 
or opt out of exporting metrics all together.

## Default metrics pipeline config

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

## Available Metrics

Config Sync exports the following metrics that can be customized:

### Core Metrics
- `reconciler_errors` - Number of reconciler errors
- `pipeline_error_observed` - Pipeline errors observed
- `declared_resources` - Number of declared resources
- `apply_operations_total` - Total apply operations
- `resource_fights_total` - Total resource fights
- `internal_errors_total` - Total internal errors
- `kcc_resource_count` - KCC resource count
- `resource_count` - Total resource count
- `ready_resource_count` - Ready resource count
- `cluster_scoped_resource_count` - Cluster-scoped resource count
- `resource_ns_count` - Resource namespace count
- `api_duration_seconds` - API call duration
- `apply_duration_seconds` - Apply operation duration
- `reconcile_duration_seconds` - Reconcile duration
- `rg_reconcile_duration_seconds` - Resource group reconcile duration
- `last_sync_timestamp` - Last sync timestamp

### Kustomize Metrics
- `kustomize_resource_count` - Kustomize resource count
- `kustomize_field_count` - Kustomize field count
- `kustomize_deprecating_field_count` - Kustomize deprecating field count
- `kustomize_simplification_adoption_count` - Kustomize simplification adoption count
- `kustomize_builtin_transformers` - Kustomize builtin transformers
- `kustomize_helm_inflator_count` - Kustomize Helm inflator count
- `kustomize_base_count` - Kustomize base count
- `kustomize_patch_count` - Kustomize patch count
- `kustomize_ordered_top_tier_metrics` - Kustomize ordered top tier metrics
- `kustomize_build_latency` - Kustomize build latency

### Additional Metrics
- `parser_duration_seconds` - Parser duration
- `remediate_duration_seconds` - Remediate duration
- `resource_conflicts_total` - Resource conflicts total

## Custom Monitoring Configuration

### Step 1: Get the current ConfigMap as template

```bash
kubectl get cm otel-collector-googlecloud -n config-management-monitoring -o yaml > otel-collector-config.yaml
```

### Step 2: Change the ConfigMap name

Change the `.metadata.name` to `otel-collector-custom`:

```yaml
metadata:
  name: otel-collector-custom
  namespace: config-management-monitoring
```

### Step 3: Modify the configuration

#### Adding or Removing Metrics

To **add or drop metrics** from the available metrics list, modify the `include` section under the
[filter processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/filterprocessor)
configuration:

```yaml
processors:
  filter/cloudmonitoring:
    metrics:
      include:
        match_type: strict
        metric_names:
          - reconciler_errors
          ...
          - api_duration_seconds
          - crd_count  # <-- Add your custom metric here
```

#### Using Exclude Filters

You can also use exclude filters to remove specific metrics:

```yaml
processors:
  filter/cloudmonitoring:
    metrics:
      exclude:
        match_type: regexp
        metric_names:
          - kustomize.*  # Exclude all Kustomize metrics
```

#### Disabling Cloud Monitoring Export

To **turn off the custom metrics** in Cloud Monitoring, remove this section:

```yaml
# Remove this entire section
metrics/cloudmonitoring:
  receivers: [opencensus]
  processors: [batch, filter/cloudmonitoring, metricstransform/cloudmonitoring, resourcedetection]
  exporters: [googlecloud]
```

#### Disabling Cloud Monarch Export

To **turn off the report to Cloud Monarch**, remove this section:

```yaml
# Remove this entire section
metrics/kubernetes:
  receivers: [opencensus]
  processors: [batch, filter/kubernetes, metricstransform/kubernetes, resourcedetection]
  exporters: [googlecloud/kubernetes]
```

### Step 4: Apply the ConfigMap

```bash
kubectl apply -f otel-collector-config.yaml
```
This step spawns a new ConfigMap with name `otel-collector-custom` that is recognized by Config Sync, which overrides the `otel-collector-googlecloud` ConfigMap.

### Step 5: Optional: Restart the OpenTelemetry Collector

The otel-collector Pod should automatically restart when the ConfigMap is updated. If the `otel-collector` deployment does not restart automatically, manually restart the OpenTelemetry Collector Pod to apply the new configuration:

```bash
kubectl rollout restart deployment otel-collector -n config-management-monitoring
```

### Step 6: Verify the configuration

Check that everything is running correctly:

```bash
# Check all objects are running
kubectl get all -n config-management-monitoring

# Verify the custom ConfigMap exists
kubectl get cm -n config-management-monitoring | grep otel-collector-custom

# Check the collector logs for any errors
kubectl logs -n config-management-monitoring deployment/otel-collector
```

## Minimal Custom Configuration Example

Here is a bare minimum custom ConfigMap example:

```yaml
# otel-collector-custom-cm.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: otel-collector-custom
  namespace: config-management-monitoring
  labels:
    app: opentelemetry
    component: otel-collector
data:
  otel-collector-config.yaml: |
    receivers:
      opencensus:
    exporters:
      debug:
        verbosity: detailed
        sampling_initial: 5
        sampling_thereafter: 200
    processors:
      batch:
      pipelines:
        metrics:
          receivers: [opencensus]
          processors: [batch]
          exporters: [debug]
```

## Troubleshooting

### Common Issues

1. **Collector not restarting**: 
    Ensure there is exactly one Pod with status `Running` in the `config-management-monitoring` namespace. If not, investigate and resolve issues such as resource constraints or ConfigMap configuration errors.
    ```
    kubectl get pods -n config-management-monitoring
    ```
    Expected output like
    ```
    NAME                              READY   STATUS    RESTARTS   AGE
    otel-collector-54c688bff8-swn7s   1/1     Running   0          18h
    ```
    If a new Pod is stuck in an error state, review its logs to diagnose the issue.
    ```
    kubectl logs deployment/otel-collector -n config-management-monitoring
    ```
2. **Metrics not appearing**: 
    a. Check the collector logs for configuration errors
    ```
    kubectl logs deployment/otel-collector -n config-management-monitoring"
    ```
    b. Use the [debug exporter](https://github.com/open-telemetry/opentelemetry-collector/tree/main/exporter/debugexporter) to output detailed metric logs within the container, which can help diagnose issues.
3. **Permission issues**: Verify [permission](https://cloud.google.com/kubernetes-engine/enterprise/config-sync/docs/how-to/monitor-config-sync-cloud-monitoring#metric-permission) is properly configured

### Verification Commands

```bash
# Check if the custom ConfigMap exists
kubectl get cm otel-collector-custom -n config-management-monitoring

# View the ConfigMap content
kubectl get cm otel-collector-custom -n config-management-monitoring -o yaml

# Check collector logs
kubectl logs -n config-management-monitoring deployment/otel-collector

# Check collector status
kubectl get pods -n config-management-monitoring -l app=opentelemetry

# Test metrics endpoint
kubectl port-forward -n config-management-monitoring svc/otel-collector 8675:8675
curl http://localhost:8675/metrics
```

### Version Compatibility Notes

- **Config Sync v1.12.0+**: Uses `googlecloud` exporter (recommended)
- **Config Sync v1.11.x and earlier**: Uses `stackdriver` exporter (deprecated)
- **Config Sync v1.8+**: Custom monitoring support available

## Additional Resources

- [OpenTelemetry Collector Documentation](https://opentelemetry.io/docs/collector/)
- [Google Cloud Monitoring Documentation](https://cloud.google.com/monitoring)
- [Config Sync Monitoring Guide](http://cloud/anthos-config-management/docs/how-to/monitoring-config-sync)
- [Available Config Sync Metrics](http://cloud/anthos-config-management/docs/how-to/monitoring-config-sync#metrics)
