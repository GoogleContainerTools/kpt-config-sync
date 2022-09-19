# Config Sync Metric Resource Attributes

Config Sync uses the Open Telemetry Collector (otel-collector) to collect and
export metrics from its various components to one or more destinations. 

Open Telemetry resource attributes uniquely identify a metrics source. 

## Pipeline

The metric pipeline:

component binary -> otel agent sidecar -> otel collector

## Agent Resource Attributes

Since the Component and Agent share the same Pod, most of the resource
attributes can be added by the Agent without needing to program the capability
into each component binary. So these resource attributes are added to metrics
from all components.

The GCP resource processor in the otel agent sidecar adds the following
resource attributes, if using GKE:

- cloud.provider
- cloud.platform
- cloud.account.id
- cloud.region OR cloud.availability_zone
- host.id
- host.name (when not using workload identity)
- k8s.cluster.name

## Component Resource Attributes

### Reconciler Resource Attributes

The reconciler binary currently adds the following resource attrbutes:

- k8s.pod.name (the name of the reconciler Deployment)
- k8s.pod.namespace (the namespace of the RootSync or RepoSync)

**WARNING:** These resource attributes are pending change to match open
telemetry conventions.

### Reconciler Resource Attributes

The hydration-controller binary (a reconciler sidecar) currently adds the
following resource attrbutes:

- k8s.pod.name ("kmetrics-manager")
- k8s.pod.namespace ("kmetrics-system")

**WARNING:** These resource attributes are pending change to match open
telemetry conventions.
