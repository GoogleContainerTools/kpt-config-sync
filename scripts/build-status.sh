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

# This script prints out the build status of Config Sync
# Intended for developer use to help understand the build state

set -euo pipefail

scripts_dir="$(dirname "$(realpath "$0")")"
# shellcheck source=scripts/lib/manifests.sh
source "${scripts_dir}/lib/manifests.sh"

# convenience function for printing two args where the second string is aligned
function pretty_print {
  printf "%-50s %s\n" "$1:" "$2"
}

function local_image_exists {
  image="$1"
  docker image inspect "${image}" &> /dev/null
}

function remote_image_exists {
  image="$1"
  flags=()
  # must pass --insecure flag for local registry (e.g. localhost:5000)
  [[ "${image}" == "localhost"* ]] && flags+=("--insecure")
  docker manifest inspect "${flags[@]}" "${image}" &> /dev/null
}

pretty_print "Current commit" "$(git describe --tags --always --dirty --long)"

read -r -a images <<< "$(config_sync_images)"
[[ ${#images[@]} -eq 0 ]] && exit 1
declare -A status_map
cs_tag=""

echo -n "Scanning" >&2
for image in "${images[@]}"; do
  echo -n "."
  if [[ "${image}" == *"/reconciler-manager:"* ]]; then
    cs_tag="${image##*:}"
  fi
  status=()
  local_image_exists "${image}" && status+=("LOCAL")
  remote_image_exists "${image}" && status+=("REMOTE")
  [[ ${#status[@]} == 0 ]] && status+=("NOT_FOUND")
  status_map[${image}]="$(printf "%s" "${status[@]}")"
done

echo # done scanning
pretty_print "Build version (reconciler-manager)" "${cs_tag}"

(
  echo -e "IMAGE\tSTATUS"
  (
    for image in "${!status_map[@]}"; do
      echo -e "${image}\t${status_map[${image}]}"
    done
  ) | sort
) | column -ts $'\t'
