# Installing Config Sync manually

This document is a guide on how to manually install Config Sync on a Kubernetes
cluster without Fleet Management.

The recommended approach is to [install Config Sync managed by fleet](https://cloud.google.com/kubernetes-engine/enterprise/config-sync/docs/how-to/installing-config-sync).

This document includes configurations for some common user journeys, but is not
an exhaustive list of how a Config Sync installation can be customized.

## Pre-requisites

This guide requires the following command line tools:

- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [kustomize](https://github.com/kubernetes-sigs/kustomize)

## Uninstall Config Management

**Prerequisites**:
- Set the kubectl context in your shell to the desired cluster before proceeding.
- Ensure [hierarchy controller is disabled](https://cloud.google.com/kubernetes-engine/enterprise/config-sync/docs/how-to/migrate-hierarchy-controller#kubectl) before proceeding.


If you have previously installed Config Management on your cluster, Config Management
must first be uninstalled before following this guide to install/upgrade Config Sync.

You can check if Config Management is installed on your cluster with the following command:

```shell
kubectl get configmanagement
```

If the output is not empty, Config Management is currently installed on the cluster
and must be uninstalled before proceeding. To uninstall Config Management, there are
two options:
- Use `nomos migrate --remove-configmanagement`
- Use the [uninstall script](uninstall_configmanagement.sh) provided in this directory.

### Using `nomos migrate`

Install the [nomos CLI](https://cloud.google.com/kubernetes-engine/enterprise/config-sync/docs/how-to/nomos-command)
of version 1.20.0 or greater. Then run the following command to update the cluster
in your current kubectl context:

```shell
nomos migrate --remove-configmanagement
```

### Using the uninstall shell script

The `nomos` approach is recommended, however a simple shell script is also provided
as an alternative approach that does not require installing the `nomos` binary.
The script can be run directly to update the cluster in your current kubectl context:

```shell
./uninstall_configmanagement.sh
```

## Install Config Sync

### Rendering the installation manifest

A [kustomization file](kustomization.yaml) is provided in this directory with
some common use cases commented out. Edit the kustomization file accordingly for
any desired customizations before installing.

Once the kustomization file is ready, the manifests can be rendered using `kustomize`:

```shell
kustomize build . > config-sync-install.yaml
```

The rendered manifests are now written to the `config-sync-install.yaml` file. This
file may be inspected/reviewed before applying to the cluster.

Optional: If you want Config Sync deployments to use a private registry
rather than the default registry, the following command can be used to replace
the image URLs for all deployments:

```shell
kustomize build . | sed -e "s|gcr.io/config-management-release/|[*REGISTRY*]/|g" > config-sync-install.yaml
```

### Apply the manifest to the cluster

Once you are ready to apply the manifests to the cluster and install Config Sync,
the manifests can be applied directly with `kubectl`:

```shell
kubectl apply -f config-sync-install.yaml
```

