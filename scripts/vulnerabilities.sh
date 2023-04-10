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

# This script prints a table of fixable vulnerabilities of medium severity or
# higher, based on the latest or specified git tag.
#
# USAGE: $ scripts/vulnerabilities.sh [IMAGE_TAG]
#
# The IMAGE_TAG is optional. If not specified, it defaults to the latest tag of
# the current local environment.
#
# The GCP_PROJECT environment variable is used to configure the project that
# hosts the container images in Google Container Registry.
# Default: config-management-release
#
# Requires gcloud authn/authz to read the results of vulnerability scans from
# Google Container Registry in the specified project.
# Required access:
# - Storage Object Viewer on "artifacts.${GCP_PROJECT}.appspot.com"
# - Container Registry Service Agent on Container Registry API
#   (containerregistry.googleapis.com)
# - Container Analysis Service Agent on Container Analysis API
#   (containerscanning.googleapis.com)

set -euo pipefail

configsync_latest_image_tag="$(git describe --tags --abbrev=0)"
configsync_image_tag="${1:-${configsync_latest_image_tag}}"

GCP_PROJECT="${GCP_PROJECT:-config-management-release}"

# find the tag for the gcenode-askpass-sidecar image by reading it from a Go constant.
# This sidecar is injected at runtime. So it's not in any local manifest files.
find_askpass_image_tag() {
  grep -ohE --color=never 'gceNodeAskpassImageTag\s*=.*' pkg/reconcilermanager/controllers/gcenode_askpass_sidecar.go |
    sed -E 's/gceNodeAskpassImageTag\s*=\s*"(\S+)"/\1/'
}

GCENODE_ASKPASS_TAG="${GCENODE_ASKPASS_TAG:-$(find_askpass_image_tag)}"

# TODO: share with Makefile to keep in sync
configsync_images=(
  "gcr.io/${GCP_PROJECT}/reconciler"
  "gcr.io/${GCP_PROJECT}/reconciler-manager"
  "gcr.io/${GCP_PROJECT}/admission-webhook"
  "gcr.io/${GCP_PROJECT}/hydration-controller"
  "gcr.io/${GCP_PROJECT}/hydration-controller-with-shell"
  "gcr.io/${GCP_PROJECT}/oci-sync"
  "gcr.io/${GCP_PROJECT}/helm-sync"
  "gcr.io/${GCP_PROJECT}/nomos"
)

# Build a full list of images with tags
images=(
  "gcr.io/${GCP_PROJECT}/gcenode-askpass-sidecar:${GCENODE_ASKPASS_TAG}"
)

# find the "name:tag" of images in the manifests directory.
find_manifest_images() {
  grep -rohE --color=never "image:\s+\S+:\S+" manifests/**/*.yaml |
    awk '{print $2}' | sort | uniq
}

# Add the specified tag to all configsync images
for image_name in "${configsync_images[@]}"; do
  images+=("${image_name}:${configsync_image_tag}")
done

# Add dependencies from the manifests
while IFS='' read -r image; do
  images+=("${image}")
done <<<"$(find_manifest_images)"

fixable_total=0
declare -A vuln_map

echo -n "Scanning" >&2

# Sum the fixable vulnerabilities with severity CRITICAL, HIGH, or MEDIUM
for image in "${images[@]}"; do
  echo -n "."
  if [[ "${image}" == "gcr.io/${GCP_PROJECT}/"* ]]; then
    vulnerabilities=$(gcloud beta container images describe --project "${GCP_PROJECT}" --show-package-vulnerability --format json --verbosity error "${image}" |
      jq -r '.package_vulnerability_summary.vulnerabilities')
    fixable=$(echo "${vulnerabilities}" |
      jq -r 'with_entries(select(.key == "CRITICAL" or .key == "HIGH" or .key == "MEDIUM")) | select(.vulnerability != {}) | map(map(select(.vulnerability.packageIssue[].fixAvailable == true)) | length) + [0] | add')
    vuln_map[${image}]=${fixable}
    fixable_total=$((fixable_total + fixable))
  else
    # Images from other repos are ignored, because we can't remotely scan them with GCR.
    vuln_map[${image}]="UNKNOWN"
  fi
done

echo # done scanning
(
  echo -e "IMAGE\tVULNERABILITIES"
  (
    for image in "${!vuln_map[@]}"; do
      echo -e "${image}\t${vuln_map[${image}]}"
    done
  ) | sort
) | column -ts $'\t'

if [[ "${fixable_total}" != "0" ]]; then
  echo "ERROR: ${fixable_total} critical, high, or medium vulnerabilities are fixable" >&2
  exit 1
fi
