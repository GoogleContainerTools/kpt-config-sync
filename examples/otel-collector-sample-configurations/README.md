# Purpose of this directory

This folder provides sample [custom monitoring configurations](http://cloud/anthos-config-management/docs/how-to/monitor-config-sync-custom)
for Config Sync. These examples are intended for your convenience. While Config
Sync strives to keep them updated, always consult the [latest configuration](https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/pkg/metrics/otel.go).

# Available Configurations

_otel-collector-monarch.yaml_: Serves as a template for exporting metrics
exclusively to Google Cloud Monarch and Prometheus.

_otel-collector-prometheus.yaml_: Serves as a template for exporting metrics
exclusively to Prometheus.

# Instructions

* **Apply ConfigMap**: Apply the desired ConfigMap to your cluster.
* **Restart otel-collector**: The otel-collector deployment should restart automatically. If it doesn't, execute:
```
kubectl rollout restart deployment otel-collector -n config-management-monitoring
```
# Removal

* **Delete ConfigMap**: Remove the ConfigMap from your cluster.
* **Restart otel-collector**: The otel-collector deployment should restart automatically. If it doesn't, use the same kubectl rollout restart command as above.