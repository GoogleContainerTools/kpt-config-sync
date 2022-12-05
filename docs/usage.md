# Using Open Source Config Sync

This document provides instructions on how to use Config Sync in a Kubernetes
cluster. This assumes Config Sync has already been [installed on the cluster](./installation.md).
Installing Config Sync creates the Custom Resource Definitions (CRDs) which are
used to configure Config Sync.

## Config Sync CRDs

Config Sync uses a concept of [Root repositories and Namespace repositories]. These
concepts map to the `RootSync` and `RepoSync` CRDs, respectively.

[Installing Config Sync using Anthos Config Management] (ACM) includes the
`ConfigManagement` CRD and ACM Operator, but those are not included in OSS installation.

## API Fields

See [`RootSync`/`RepoSync` fields] for a high level overview of the API fields exposed by the
`RootSync` and `RepoSync` objects. References to the `ConfigManagement` CRD and ACM Operator
(i.e. the `config-management-operator` Deployment in the `config-management-system` namespace)
can be ignored since they are not included in OSS installation.

For the most accurate API information based on which version is installed on
your cluster, you can also inspect the CRDs yourself.

```shell
kubectl get crd rootsyncs.configsync.gke.io -oyaml
```

```shell
kubectl get crd reposyncs.configsync.gke.io -oyaml
```

## Configuring Config Sync

See [Configure syncing from multiple repositories] for some patterns and examples
on how to configure RootSync and RepoSync objects.

[`RootSync`/`RepoSync` fields]: https://cloud.google.com/anthos-config-management/docs/reference/rootsync-reposync-fields
[Configure syncing from multiple repositories]: https://cloud.google.com/anthos-config-management/docs/how-to/multiple-repositories
[Root repositories and Namespace repositories]: https://cloud.google.com/anthos-config-management/docs/config-sync-overview#repositories
[Installing Config Sync using Anthos Config Management]: https://cloud.google.com/anthos-config-management/docs/how-to/installing-config-sync