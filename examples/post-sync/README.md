# Sync Status Watch Controller

A Kubernetes controller that monitors and reports sync error status for Config Sync's `RootSync` and `RepoSync` resources.

> **Note**: ⚠️ **This component is not released with Config Sync**.
> - It is **only compatible** with the **Config Sync API at same branch HEAD**.
> - **Backward compatibility is not supported at this moment**.
> - **Version management and continuous builds** of this controller **must be handled by the user**.

## Prerequisites

- Go 1.24+
- Docker
- kubectl
- Access to a Kubernetes cluster with Config Sync installed and configured
- **Container registry (e.g., Google Artifact Registry)**
  *This guide assumes you have a registry configured and authenticated.*

## Environment Variables
Configure these before building/deploying:
```sh
export REGION="us-central1"             # Your registry region (e.g., GAR)
export PROJECT_ID="peip-monitor"     # Your GCP project ID
export GAR_REPO_NAME="sync-status-watch"    # Name of your GAR repository
export IMAGE_NAME="sync-status-watch"   # Docker image name
export IMAGE_TAG="latest"               # Docker image tag
```

## Quick Start
### **Build and push the image** (using the provided `Makefile`):
   ```sh
   make build push
   ```

### **Deploy to Kubernetes**:

   Replace `SYNC_STATUS_WATCH_CONTROLLER_IMAGE_REGISTRY` with your container image URL (e.g., us-central1-docker.pkg.dev/YOUR_PROJECT_ID/repo-name/image:tag).
   ```sh
   kubectl apply -f sync-watch-manifest.yaml
   ```
   Or with `sed` installed
   ```sh
   make deploy
   ```

### **Verify deployment**:
   ```sh
   kubectl get pods -n sync-status-watch
   kubectl logs -f deployment/sync-status-watch -n sync-status-watch
   ```

### **Sample Output**:

Successful startup
```
{"level":"info","ts":1744307308.0837288,"caller":"/main.go:246","msg":"Starting standalone SyncStatusController"}
```

Error detected
```
{"level":"info","ts":1744307308.1975124,"caller":"/main.go:119","msg":"Sync error detected","sync":"rs-quickstart","namespace":"config-management-system","kind":"RootSync","commit":"e79b57c3705821b2734531aec690e61a369c3586","status":"failed","error":"aggregated errors: Code: 1029, Message: KNV1029: Namespace-scoped configs of the same Group and Kind MUST have unique names if they are in the same Namespace. Found 2 configs of GroupKind \"ConfigMap\" in Namespace \"config-management-monitoring\" named \"test-monitoring-cm2\". Rename or delete the duplicates to fix:\n\nsource: config-sync-quickstart/multirepo/root/monitoring-cm2-dupe.yaml\nnamespace: config-management-monitoring\nmetadata.name: test-monitoring-cm2\ngroup:\nversion: v1\nkind: ConfigMap\n\nsource: config-sync-quickstart/multirepo/root/monitoring-cm2.yaml\nnamespace: config-management-monitoring\nmetadata.name: test-monitoring-cm2\ngroup:\nversion: v1\nkind: ConfigMap\n\nFor more information, see https://g.co/cloud/acm-errors#knv1029"}
```

## Cleanup

To remove the controller:
```sh
kubectl delete -f sync-watch-manifest.yaml
kubectl delete namespace sync-status-watch
```

To delete the GAR repository (optional):
```sh
gcloud artifacts repositories delete ${GAR_REPO_NAME} --location=${REGION}
```