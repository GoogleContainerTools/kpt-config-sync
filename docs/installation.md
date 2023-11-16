# Installing Config Sync

This document provides instructions on how to install open source Config Sync in a
Kubernetes cluster. For instructions on how to install Config Sync using ACM, see
[Installing Config Sync using Anthos Config Management].

## Installing a released version

1. Find the latest Config Sync release on the [releases page]. Note the release
version that you want to install.
2. Install the released version by applying the manifest to your cluster.
```shell
# Set the release version
export CS_VERSION=vX.Y.Z
# Apply core Config Sync manifests to your cluster
kubectl apply -f "https://github.com/GoogleContainerTools/kpt-config-sync/releases/download/${CS_VERSION}/config-sync-manifest.yaml"
```

## Building and Installing from source

This section describes how to build and install Config Sync from source. This
assumes that you have a GCP project and GCR repository to publish the Config
Sync images.

1. Follow the [development instructions] to build Config Sync from source.
2. Upon success the docker images are published to your GCR repository and the
KRM manifests are placed in `./.output/staging/oss`. The manifests can be
applied directly to your cluster.
```shell
kubectl apply -f ./.output/staging/oss/config-sync-manifest.yaml
```
3. If the Pod is in a `ImagePullBackoff` or `ErrImagePull` state, that indicates
   the Compute Engine default service account doesn't have permission to pull
   the image from your private registry. You need to grant the `Storage Object
   Viewer` role to the service account.

   * Using Cloud Console: Find the compute service account by going to IAM &
     Admin on your project and grant the `Storage Object Viewer` role. The
     service account should look like
     `<project-number>-compute@developer.gserviceaccount.com`.

   * Using gcloud:

     ```
     gcloud projects add-iam-policy-binding [*PROJECT_ID*] --member=serviceAccount:[*PROJECT_NUMBER*]-compute@developer.gserviceaccount.com --role=roles/storage.objectViewer
     ```
   
## Using Config Sync

See [Using Config Sync](./usage.md) for more information on how to configure/use
Config Sync once it's installed.

[releases page]: https://github.com/GoogleContainerTools/kpt-config-sync/releases
[development instructions]: development.md
[Installing Config Sync using Anthos Config Management]: https://cloud.google.com/anthos-config-management/docs/how-to/installing-config-sync
