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

# Update Metrics Server YAML from GitHub

set -euo pipefail

pkg_dir="$(dirname "$(realpath "$0")")"
cd "${pkg_dir}"

TARGET_REGISTRY="registry.k8s.io/metrics-server" # TODO: re-push to CI registry
TARGET_IMAGE_TAG="v0.6.3"
DEFAULT_REGISTRY="registry.k8s.io/metrics-server"

# Download and render the template yaml, replace the images, and write to disk.
# WARNING: Only use insecure TLS for testing!
# TODO: How do you specify version in helm?
helm template "test" \
    --set "args={--kubelet-insecure-tls}" \
    --repo https://kubernetes-sigs.github.io/metrics-server \
    metrics-server \
    | sed -e "s,${DEFAULT_REGISTRY}/\([a-z-]*\):.*,${TARGET_REGISTRY}/\1:${TARGET_IMAGE_TAG}," \
    > components.yaml
