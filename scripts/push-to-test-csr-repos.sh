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

# This script pushes the kustomize-components configs to the Cloud Source Repository.
# The source configs are stored in the e2e/testdata/hydration/kustomize-components folder, and used in the e2e test.
#
# Pre-requisite:
# 1. GCP_PROJECT is set.
# 2. The CSR repository has already been created in the ${GCP_PROJECT} project.
# 3. You have the write permission to the CSR repository.

REPO_DIR="$(readlink -f "$(dirname "$0")")/.."

if [ "${GCP_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: GCP_PROJECT is unset. Please provide the GCP_PROJECT env variable to specify your GCP project' >&2
  exit 1
fi

KUSTOMIZE_COMPONENTS_PACKAGE_NAME=kustomize-components
KUSTOMIZE_COMPONENTS_DIR=${REPO_DIR}/e2e/testdata/hydration/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}

TMP_DIR=/tmp/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}
KUSTOMIZE_COMPONENTS_REPO_URL=https://source.developers.google.com/p/${GCP_PROJECT}/r/${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}

echo "Pushing '${KUSTOMIZE_COMPONENTS_PACKAGE_NAME}' to cloud source repository '${KUSTOMIZE_COMPONENTS_REPO_URL}'"
rm -rf "${TMP_DIR}" && cp -r "${KUSTOMIZE_COMPONENTS_DIR}" "${TMP_DIR}"
pushd "${TMP_DIR}"
gcloud init && git config --global credential.https://source.developers.google.com.helper gcloud.sh
git init
git add -A && git commit -m "Add DRY configs for kustomize components"
git remote add google "${KUSTOMIZE_COMPONENTS_REPO_URL}"
git push --all -f google
popd
