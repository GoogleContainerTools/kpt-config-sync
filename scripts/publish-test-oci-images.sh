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

# This script builds and pushes the test OCI images to GAR and GCR.
# The OCI images are built from the e2e/testdata folder, and used in the e2e test.
#
# Pre-requisite:
# 1. GCP_PROJECT is set.
# 2. The GAR/GCR repositories have already been created in the ${GCP_PROJECT} project.
# 3. You have the write permission to the GAR/GCR repositories.

set -euo pipefail

if [ "${GCP_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: GCP_PROJECT is unset. Please provide the GCP_PROJECT env variable to specify your GCP project' >&2
  exit 1
fi

LOCATION=us

REPO_DIR="$(readlink -f "$(dirname "$0")")/.."
KUSTOMIZE_COMPONENTS_PACKAGE_NAME=kustomize-components
KUSTOMIZE_COMPONENTS_DIR=${REPO_DIR}/e2e/testdata/hydration/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}
KUSTOMIZE_COMPONENTS_PUBLIC_AR_IMAGE=${LOCATION}-docker.pkg.dev/${GCP_PROJECT}/config-sync-test-public/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}
KUSTOMIZE_COMPONENTS_PUBLIC_GCR_IMAGE=gcr.io/${GCP_PROJECT}/config-sync-test/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}
KUSTOMIZE_COMPONENTS_PRIVATE_AR_IMAGE=${LOCATION}-docker.pkg.dev/${GCP_PROJECT}/config-sync-test-private/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}:v1
KUSTOMIZE_COMPONENTS_PRIVATE_GCR_IMAGE=${LOCATION}.gcr.io/${GCP_PROJECT}/config-sync-test/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}:v1


crane auth login ${LOCATION}-docker.pkg.dev -u oauth2accesstoken -p "$(gcloud auth print-access-token)"
crane auth login ${LOCATION}.gcr.io -u oauth2accesstoken -p "$(gcloud auth print-access-token)"

pushd "${KUSTOMIZE_COMPONENTS_DIR}"
echo "Pushing '${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}' to public GCR repository '${KUSTOMIZE_COMPONENTS_PUBLIC_GCR_IMAGE}'"
crane append -f <(tar -f - -c .) -t "${KUSTOMIZE_COMPONENTS_PUBLIC_GCR_IMAGE}"
echo "Pushing '${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}' to public AR repository '${KUSTOMIZE_COMPONENTS_PUBLIC_AR_IMAGE}'"
crane append -f <(tar -f - -c .) -t "${KUSTOMIZE_COMPONENTS_PUBLIC_AR_IMAGE}"
echo "Pushing '${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}' to private GCR repository '${KUSTOMIZE_COMPONENTS_PRIVATE_GCR_IMAGE}'"
crane append -f <(tar -f - -c .) -t "${KUSTOMIZE_COMPONENTS_PRIVATE_GCR_IMAGE}"
echo "Pushing '${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}' to private AR repository '${KUSTOMIZE_COMPONENTS_PRIVATE_AR_IMAGE}'"
crane append -f <(tar -f - -c .) -t "${KUSTOMIZE_COMPONENTS_PRIVATE_AR_IMAGE}"
popd

BOOKINFO_REPO_PACKAGE_NAME=namespace-repo-bookinfo
BOOKINFO_REPO_DIR=${REPO_DIR}/e2e/testdata/${BOOKINFO_REPO_PACKAGE_NAME}
BOOKINFO_REPO_PUBLIC_AR_IMAGE=${LOCATION}-docker.pkg.dev/${GCP_PROJECT}/config-sync-test-public/${BOOKINFO_REPO_PACKAGE_NAME}

pushd "${BOOKINFO_REPO_DIR}"
echo "Pushing '${BOOKINFO_REPO_PACKAGE_NAME}' to public AR repository '${BOOKINFO_REPO_PUBLIC_AR_IMAGE}'s"
crane append -f <(tar -f - -c .) -t "${BOOKINFO_REPO_PUBLIC_AR_IMAGE}"
popd
