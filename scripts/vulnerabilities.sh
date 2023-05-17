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
# USAGE: $ scripts/vulnerabilities.sh
#
# The script will look for the rendered manifests at .output/staging to parse
# image tags.
#
# Requires gcloud authn/authz to read the results of vulnerability scans from
# GCR/GAR in the specified project.
# Required access:
# - Storage Object Viewer on "artifacts.${GCP_PROJECT}.appspot.com" (GCR - if applicable)
# - Artifact Registry Reader (GAR - if applicable)
# - Container Analysis Occurrences Viewer

set -euo pipefail

# find the tag for the gcenode-askpass-sidecar image by reading it from a Go constant.
# This sidecar is injected at runtime. So it's not in any local manifest files.
find_askpass_image_tag() {
  grep -ohE --color=never 'gceNodeAskpassImageTag\s*=.*' pkg/reconcilermanager/controllers/gcenode_askpass_sidecar.go |
    sed -E 's/gceNodeAskpassImageTag\s*=\s*"(\S+)"/\1/'
}

GCENODE_ASKPASS_TAG="${GCENODE_ASKPASS_TAG:-$(find_askpass_image_tag)}"

# Build a full list of images with tags
images=(
  "gcr.io/config-management-release/gcenode-askpass-sidecar:${GCENODE_ASKPASS_TAG}"
)

# find the "name:tag" of images in the manifests directory.
find_manifest_images() {
  grep -rohE --color=never "image:\s+\S+:\S+" .output/staging/**/*.yaml |
    awk '{print $2}' | sort | uniq
}

registry=""
cs_tag=""

echo "++++ Parsing image tags from rendered manifests at: .output/staging"

manifest_images="$(find_manifest_images || echo -n "")"
if [[ "${manifest_images}" == "" ]]; then
  echo "++++ No images found in rendered manifests at: .output/staging"
  echo "++++ For instructions on how to build or pull manifests, see https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/docs/development.md#build"
  exit 1
fi

# Add dependencies from the manifests
while IFS='' read -r image; do
  images+=("${image}")
  # use reconciler-manager image as an representative of the CS registry/tag
  if [[ "${image}" == *"/reconciler-manager:"* ]]; then
    registry="${image%/reconciler-manager:*}"
    cs_tag="${image#*:}"
  fi
done <<<"${manifest_images}"

# add CS images not included in manifest
images+=("${registry}/nomos:${cs_tag}")
images+=("${registry}/hydration-controller-with-shell:${cs_tag}")

fixable_total=0
declare -A vuln_map

echo -n "Scanning" >&2

# Sum the fixable vulnerabilities with severity CRITICAL, HIGH, or MEDIUM
for image in "${images[@]}"; do
  echo -n "."
  vulnerabilities=$(gcloud beta container images describe --show-package-vulnerability --format json --verbosity error "${image}" |
    jq -r '.package_vulnerability_summary.vulnerabilities')
  fixable=$(echo "${vulnerabilities}" |
    jq -r 'with_entries(select(.key == "CRITICAL" or .key == "HIGH" or .key == "MEDIUM")) | select(.vulnerability != {}) | map(map(select(.vulnerability.packageIssue[].fixAvailable == true)) | length) + [0] | add')
  vuln_map[${image}]=${fixable}
  fixable_total=$((fixable_total + fixable))
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
