#!/bin/bash
# Copyright 2023 Google LLC
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

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

# Required environment variables
# shellcheck disable=SC2269 # validate non-null with errexit + nounset
HELM_VERSION=${HELM_VERSION}
# shellcheck disable=SC2269 # validate non-null with errexit + nounset
INSTALL_DIR=${INSTALL_DIR}

# Optional environment variables
TMPDIR=${TMPDIR:-/tmp}
OUTPUT_DIR=${OUTPUT_DIR:-${REPO_ROOT}/.output}
STAGING_DIR=${STAGING_DIR:-${OUTPUT_DIR}/third_party/helm}

function helm_version_installed() {
    local version
    version=$("${INSTALL_DIR}/helm" version --short)
    echo "${version%+*}" # remove commit suffix
}

# Check installed version
if [[ -f "${INSTALL_DIR}/helm" ]] && [[ -x "${INSTALL_DIR}/helm" ]]; then
    if [[ "$(helm_version_installed)" == "${HELM_VERSION}" ]]; then
        echo "helm version: ${HELM_VERSION} (already installed)"
        exit 0
    fi
fi

HELM_TARBALL_URL=gs://config-management-release/config-sync/helm/tag/${HELM_VERSION}/helm-${HELM_VERSION}-linux-amd64.tar.gz
HELM_CHECKSUM_URL=${HELM_TARBALL_URL}.sha256
HELM_TARBALL=${TMPDIR}/helm-${HELM_VERSION}-linux-amd64.tar.gz
HELM_CHECKSUM=${HELM_TARBALL}.sha256

function cleanup() {
    rm -f "${HELM_TARBALL}"
    rm -f "${HELM_CHECKSUM}"
}
trap cleanup EXIT

echo "Downloading helm ${HELM_VERSION}"
gsutil cp "${HELM_TARBALL_URL}" "${HELM_TARBALL}"
gsutil cp "${HELM_CHECKSUM_URL}" "${HELM_CHECKSUM}"

echo "Verifying helm checksum"
echo "$(cat "${HELM_CHECKSUM}")  ${HELM_TARBALL}" | sha256sum -c

echo "Installing helm"
mkdir -p "${STAGING_DIR}"
tar -zxvf "${HELM_TARBALL}" -C "${STAGING_DIR}"
chmod a+x "${STAGING_DIR}/helm"
cp "${STAGING_DIR}/helm" "${INSTALL_DIR}/helm"

echo "helm version: $(helm_version_installed)"
