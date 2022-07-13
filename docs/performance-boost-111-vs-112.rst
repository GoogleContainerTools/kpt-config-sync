Config Sync Performance boost v1.11 vs v1.12

Config Sync v1.12 contains several changes to let it sync faster with less total memory consumption. Here are some test results that shows the performance boost.

Sync Latency Median (unit: second)
+-------------------+----------------+----------------+---------------+
| Test Name         |         v1.11  |         v1.12  |         Diff  |
+===================+================+================+===============+
| ConfigMap 10K 1N  |          170   |          120   |       -29.4%  |
| ConfigMap 10K 10N |          170   |          120   |       -29.4%  |
| CR 3K             |          210   |           90   |       -57.1%  |
| Large CR 1K       |          240   |          100   |       -58.3%  |
| Deployment 3k     |          100   |           50   |       -50.0%  |
+-------------------+----------------+----------------+---------------+

Reconciler Peak Memory Usage (unit: MB)
+-------------------+----------------+----------------+---------------+
| Test Name         |         v1.11  |         v1.12  |         Diff  |
+===================+================+================+===============+
| ConfigMap 10K 1N  |          529   |          323   |       -38.9%  |
| ConfigMap 10K 10N |          551   |          358   |       -35.0%  |
| CR 3K             |          408   |          441   |       +8.1%   |
| Large CR 1K       |         1505   |         1998   |       +32.8%  |
| Deployment 3k     |         1349   |          426   |       -68.4%  |
+-------------------+----------------+----------------+---------------+


Sync Latency:

Sync latency is defined by the time it takes for a Config Sync reconciler to apply all resources from a git repo to a cluster.
It doesn’t include the time it takes for all resources to finish reconciliation.
It doesn’t cover the use cases when dependencies and waits are involved when syncing resources.
It doesn’t include the additional API request latency from admission webhooks.


Tests:

ConfigMap 10K 1N: Sync 10000 ConfigMaps to a cluster in 1 namespace after the namespace finishing reconciliation

ConfigMap 10K 10N: Sync 10000 ConfigMaps to a cluster and spread in 10 namespaces after the 10 namespaces finishing reconciliation

CR 3K: Sync 3000 Custom Resources to a cluster and spread in 3000 namespaces after the 3000 namespaces and related CRD finishing reconciliation

Large CR 1K: Sync 1000 Large Custom Resources(36 KB) to a cluster and spread in 1000 namespaces after the 1000 namespaces and related CRD finishing reconciliation

Deployment 3K: Sync 3000 Deployements to a cluster in 1 namespace after the namespace finishing reconciliation


Test Cluster:

Version: 1.22.8-gke.202
Nodes: 9(3 per zone)
Total vCPUs: 144
Total memory: 576GB