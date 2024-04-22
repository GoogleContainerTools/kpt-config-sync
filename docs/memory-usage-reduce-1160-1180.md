# Config Sync Memory Usage Reduction v1.16 vs v1.18

Config Sync v1.18 contains change to stop loading OpenAPI for schema validations when Config Sync admission webhook is not enabled. Here are some test results that shows the reduction in the memory usage.

### Root Reconciler Peak Memory Usage (unit: MB)
| Test Name      | v1.16 | v1.18 | Diff   |
|----------------|-------|-------|--------|
| AutoPilot 1.27 | 300   | 180   | -40.0% |
| AutoPilot 1.28 | 240   | 160   | -33.3% |
| AutoPilot 1.29 | 230   | 160   | -30.4% |
| Standard 1.27  | 260   | 170   | -34.6% |
| Standard 1.28  | 220   | 140   | -36.4% |
| Standard 1.29  | 210   | 140   | -33.3% |

### Test Setup

#### Test Repository

[Config Sync Quickstart](https://github.com/GoogleCloudPlatform/anthos-config-management-samples/tree/main/config-sync-quickstart)

#### Other Test Data

91 additional CRDs on cluster to stress the declared-fields annotation computation.

#### Test Clusters

| Cluster Name   | Version              | Nodes | Total vCPUs | Total memory |
|----------------|----------------------|-------|-------------|--------------|
| AutoPilot 1.27 | 1.27.11-gke.1062000  | 3     | 4           | 5GB          |
| AutoPilot 1.28 | 1.28.7-gke.1026000   | 3     | 4.75        | 5.75GB       |
| AutoPilot 1.29 | 1.29.3-gke.1093000   | 3     | 3.72        | 4.19GB       |
| Standard 1.27  | 1.27.11-gke.1062000  | 3     | 6           | 12GB         |
| Standard 1.28  | 1.28.7-gke.1026000   | 3     | 6           | 12GB         |
| Standard 1.29  | 1.29.3-gke.1093000   | 3     | 6           | 12GB         |