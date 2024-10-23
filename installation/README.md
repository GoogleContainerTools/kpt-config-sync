# Installing Config Sync manually

This document is a guide on how to manually install Config Sync on a Kubernetes
cluster.

This document includes configurations for some common user journeys, but is not
an exhaustive list of how a Config Sync installation can be customized.

## Pre-requisites

This guide requires the following command line tools:

- [kubectl](https://kubernetes.io/docs/tasks/tools/)
- [kustomize](https://github.com/kubernetes-sigs/kustomize)

## Rendering the installation manifest

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
kustomize build . | sed -e "s|CONFIG_SYNC_REGISTRY/|[*REGISTRY*]/|g" > config-sync-install.yaml
```

### Apply the manifest to the cluster

Once you are ready to apply the manifests to the cluster and install Config Sync,
the manifests can be applied directly with `kubectl`:

```shell
kubectl apply -f config-sync-install.yaml
```

