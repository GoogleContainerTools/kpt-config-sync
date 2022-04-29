# Installing Config Sync

This document provides instructions on how to install Config Sync in a
kubernetes cluster.

## Installing a released version

1. Find the latest Config Sync release on the [releases page]. Download the
manifest bundle from the release assets.
2. Install the released version by applying the manifest to your cluster.
```shell
# Create a directory to store the manifest bundle
mkdir -p <MANIFEST_DIR>
# Extract the manifest bundle
tar -xzf <MANIFEST_BUNDLE> -C <MANIFEST_DIR>
# Apply the manifests to your cluster
kubectl apply -f <MANIFEST_DIR>
```

## Building and Installing from source

This section describes how to build and install Config Sync from source. This
assumes that you have a GCP project and GCR repository to publish the Config
Sync images.

1. Follow the [development instructions] to build Config Sync from source.
2. Upon success the docker images are published to your GCR repository and the
KRM manifests are placed in `./.output/oss/manifests`. The manifests can be
applied directly to your cluster.
```shell
kubectl apply -f ./.output/oss/manifests
```

[releases page]: https://github.com/GoogleContainerTools/kpt-config-sync/releases
[development instructions]: development.md
