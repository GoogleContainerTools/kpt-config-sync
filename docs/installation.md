# Installing Config Sync

This document provides instructions on how to install Config Sync in a
kubernetes cluster.

## Installing a released version

1. Find the latest Config Sync release on the [releases page]. Download the
manifest bundle from the release assets.
2. Install the released version by applying the manifest to your cluster.
```shell
# Apply core Config Sync manifests to your cluster
kubectl apply -f path/to/config-sync-manifest.yaml
# Optional: apply acm-psp.yaml to your cluster (for k8s < 1.25)
kubectl apply -f path/to/acm-psp.yaml
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

[releases page]: https://github.com/GoogleContainerTools/kpt-config-sync/releases
[development instructions]: development.md
