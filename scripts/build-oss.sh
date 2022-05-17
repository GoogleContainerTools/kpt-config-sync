#!/bin/bash
# Copyright 2022 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -euo pipefail

# Setup

REPO_DIR="$(readlink -f "$(dirname "$0")")/.."

if [ "${GCP_PROJECT:-"unset"}" == "unset" ]; then
  if ! [ -x "$(command -v gcloud)" ]; then
    echo 'Error: gcloud is not available. Please provide the GCP_PROJECT env variable to specify your GCP project' >&2
    exit 1
  else
    GCP_PROJECT=$(gcloud config get-value project)
  fi
fi

TAG="${TAG:-"latest"}"

echo "+++ Building and pushing images"
NOMOS_TAG="gcr.io/${GCP_PROJECT}/configsync/nomos:${TAG}"
MGR_TAG="gcr.io/${GCP_PROJECT}/configsync/reconciler-manager:${TAG}"
HC_TAG="gcr.io/${GCP_PROJECT}/configsync/hydration-controller:${TAG}"
HC_WITH_SHELL_TAG="gcr.io/${GCP_PROJECT}/configsync/hydration-controller-with-shell:${TAG}"
REC_TAG="gcr.io/${GCP_PROJECT}/configsync/reconciler:${TAG}"
WEBHOOK_TAG="gcr.io/${GCP_PROJECT}/configsync/admission-webhook:${TAG}"
OCI_TAG="gcr.io/${GCP_PROJECT}/configsync/oci-sync:${TAG}"

make -C "${REPO_DIR}" image-nomos \
  NOMOS_TAG="${NOMOS_TAG}" \
  RECONCILER_TAG="${REC_TAG}" \
  HYDRATION_CONTROLLER_TAG="${HC_TAG}" \
  HYDRATION_CONTROLLER_WITH_SHELL_TAG="${HC_WITH_SHELL_TAG}"\
  RECONCILER_MANAGER_TAG="${MGR_TAG}" \
  ADMISSION_WEBHOOK_TAG="${WEBHOOK_TAG}" \
  OCI_SYNC_TAG="${OCI_TAG}"
docker push "${MGR_TAG}"
docker push "${HC_TAG}"
docker push "${HC_WITH_SHELL_TAG}"
docker push "${REC_TAG}"
docker push "${WEBHOOK_TAG}"
docker push "${OCI_TAG}"

echo "+++ Generating manifests"
cp "${REPO_DIR}"/manifests/test-resources/00-namespace.yaml "${MANIFEST_DIR}"/00-namespace.yaml
cp "${REPO_DIR}"/manifests/namespace-selector-crd.yaml "${MANIFEST_DIR}"/namespace-selector-crd.yaml
cp "${REPO_DIR}"/manifests/cluster-selector-crd.yaml "${MANIFEST_DIR}"/cluster-selector-crd.yaml
cp "${REPO_DIR}"/manifests/cluster-registry-crd.yaml "${MANIFEST_DIR}"/cluster-registry-crd.yaml
cp "${REPO_DIR}"/manifests/reconciler-manager-service-account.yaml "${MANIFEST_DIR}"/reconciler-manager-service-account.yaml
cp "${REPO_DIR}"/manifests/otel-agent-cm.yaml "${MANIFEST_DIR}"/otel-agent-cm.yaml
cp "${REPO_DIR}"/manifests/test-resources/resourcegroup-manifest.yaml "${MANIFEST_DIR}"/resourcegroup-manifest.yaml

sed -e "s|RECONCILER_IMAGE_NAME|$REC_TAG|" \
  -e "s|OCI_SYNC_IMAGE_NAME|$OCI_TAG|" \
  -e "s|HYDRATION_CONTROLLER_IMAGE_NAME|$HC_TAG|" \
  "${REPO_DIR}"/manifests/templates/reconciler-manager-configmap.yaml \
  > "${MANIFEST_DIR}"/reconciler-manager-configmap.yaml

cp "${REPO_DIR}"/manifests/reposync-crd.yaml "${MANIFEST_DIR}"/reposync-crd.yaml
cp "${REPO_DIR}"/manifests/rootsync-crd.yaml "${MANIFEST_DIR}"/rootsync-crd.yaml

sed -e "s|IMAGE_NAME|$MGR_TAG|g" "${REPO_DIR}"/manifests/templates/reconciler-manager.yaml \
  > "${MANIFEST_DIR}"/reconciler-manager.yaml

cp "${REPO_DIR}"/manifests/templates/otel-collector.yaml "${MANIFEST_DIR}"/otel-collector.yaml
cp "${REPO_DIR}"/manifests/templates/reconciler-manager/dev.yaml "${MANIFEST_DIR}"/dev.yaml

sed -e "s|IMAGE_NAME|$WEBHOOK_TAG|g" "${REPO_DIR}"/manifests/templates/admission-webhook.yaml \
  > "${MANIFEST_DIR}"/admission-webhook.yaml

echo "+++ Manifests generated in ${MANIFEST_DIR}"