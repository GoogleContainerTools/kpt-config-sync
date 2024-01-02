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
KUSTOMIZE_VERSION=${KUSTOMIZE_VERSION}
# shellcheck disable=SC2269 # validate non-null with errexit + nounset
INSTALL_DIR=${INSTALL_DIR}

# Optional environment variables
TMPDIR=${TMPDIR:-/tmp}
OUTPUT_DIR=${OUTPUT_DIR:-${REPO_ROOT}/.output}
STAGING_DIR=${STAGING_DIR:-${OUTPUT_DIR}/third_party/kustomize}

function helm_version_installed() {
    "${INSTALL_DIR}/kustomize" version
}

# Check installed version
if [[ -f "${INSTALL_DIR}/kustomize" ]] && [[ -x "${INSTALL_DIR}/kustomize" ]]; then
    if [[ "$(helm_version_installed)" == "${KUSTOMIZE_VERSION}" ]]; then
        echo "kustomize version: ${KUSTOMIZE_VERSION} (already installed)"
        exit 0
    fi
fi

KUSTOMIZE_TARBALL_URL=gs://config-management-release/config-sync/kustomize/tag/${KUSTOMIZE_VERSION}/kustomize-${KUSTOMIZE_VERSION}-linux-amd64.tar.gz
KUSTOMIZE_CHECKSUM_URL=${KUSTOMIZE_TARBALL_URL}.sha256
KUSTOMIZE_TARBALL=${TMPDIR}/kustomize-${KUSTOMIZE_VERSION}-linux-amd64.tar.gz
KUSTOMIZE_CHECKSUM=${KUSTOMIZE_TARBALL}.sha256

function cleanup() {
    rm -f "${KUSTOMIZE_TARBALL}"
    rm -f "${KUSTOMIZE_CHECKSUM}"
}
trap cleanup EXIT

echo "Downloading kustomize ${KUSTOMIZE_VERSION}"
gsutil cp "${KUSTOMIZE_TARBALL_URL}" "${KUSTOMIZE_TARBALL}"
gsutil cp "${KUSTOMIZE_CHECKSUM_URL}" "${KUSTOMIZE_CHECKSUM}"

echo "Verifying kustomize checksum"
echo "$(cat "${KUSTOMIZE_CHECKSUM}")  ${KUSTOMIZE_TARBALL}" | sha256sum -c

echo "Installing kustomize"
mkdir -p "${STAGING_DIR}"
tar -zxvf "${KUSTOMIZE_TARBALL}" -C "${STAGING_DIR}"
cp "${STAGING_DIR}/kustomize" "${INSTALL_DIR}/kustomize"

echo "kustomize version: $("${INSTALL_DIR}/kustomize" version)"
