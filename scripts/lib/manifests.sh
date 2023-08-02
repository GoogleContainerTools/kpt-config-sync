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

# find the "name:tag" of images in the manifests directory.
find_manifest_images() {
  grep -rohE --color=never "image:\s+\S+:\S+" .output/staging/oss/*.yaml |
    awk '{print $2}' | sort | uniq
}

config_sync_images() {
  # Build a full list of images with tags
  images=()

  echo "++++ Parsing image tags from rendered manifests at: .output/staging" >&2

  manifest_images="$(find_manifest_images || echo -n "")"
  if [[ "${manifest_images}" == "" ]]; then
    echo "++++ No images found in rendered manifests at: .output/staging" >&2
    echo "++++ For instructions on how to build or pull manifests, see https://github.com/GoogleContainerTools/kpt-config-sync/blob/main/docs/development.md#build" >&2
    return 1
  fi

  registry=""
  cs_tag=""
  # Add dependencies from the manifests
  while IFS='' read -r image; do
    images+=("${image}")
    # use reconciler-manager image as an representative of the CS registry/tag
    if [[ "${image}" == *"/reconciler-manager:"* ]]; then
      registry="${image%/reconciler-manager:*}"
      cs_tag="${image##*:}"
    fi
  done <<<"${manifest_images}"

  # add CS images not included in manifest
  images+=("${registry}/nomos:${cs_tag}")
  images+=("${registry}/hydration-controller-with-shell:${cs_tag}")
  echo "${images[@]}"
  return 0
}




