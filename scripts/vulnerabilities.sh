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

scripts_dir="$(dirname "$(realpath "$0")")"
# shellcheck source=scripts/lib/manifests.sh
source "${scripts_dir}/lib/manifests.sh"

read -r -a images <<< "$(config_sync_images)"
[[ ${#images[@]} -eq 0 ]] && exit 1
fixable_total=0
scan_failure_total=0
declare -A vuln_map

echo -n "Scanning" >&2

# Sum the fixable vulnerabilities with severity CRITICAL, HIGH, or MEDIUM
for image in "${images[@]}"; do
  echo -n "."
  results=$(gcloud beta container images describe --show-package-vulnerability --format json --verbosity error "${image}")
  status=$(echo "${results}" | jq -r '.discovery_summary.discovery[] | select(.noteName == "projects/goog-analysis/notes/PACKAGE_VULNERABILITY") | .discovery.analysisStatus')
  if [[ "${status}" != "FINISHED_SUCCESS" ]]; then
    vuln_map[${image}]="${status}"
    scan_failure_total=$((scan_failure_total + 1))
  else
    vulnerabilities=$(echo "${results}" | jq -r '.package_vulnerability_summary.vulnerabilities')
    fixable=$(echo "${vulnerabilities}" |
      jq -r 'select(.vulnerability != {}) | map(map(select(.vulnerability.packageIssue[].fixAvailable == true)) | length) + [0] | add')
    vuln_map[${image}]=${fixable}
    fixable_total=$((fixable_total + fixable))
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

exit_code=0

if [[ "${fixable_total}" != "0" ]]; then
  echo "ERROR: ${fixable_total} vulnerabilities are fixable" >&2
  exit_code=1
fi

if [[ "${scan_failure_total}" != "0" ]]; then
  echo "ERROR: ${scan_failure_total} images failed to be scanned" >&2
  exit_code=1
fi

exit "${exit_code}"
