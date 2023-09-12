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

# Update VPA YAML from GitHub

set -euo pipefail

vpa_dir="$(dirname "$(realpath "$0")")"
cd "${vpa_dir}"

DEFAULT_REGISTRY="registry.k8s.io/autoscaling"
TARGET_REGISTRY="registry.k8s.io/autoscaling" # TODO: re-push to CI registry
TARGET_IMAGE_TAG="0.14.0"

# TARGET_GIT_TAG="vertical-pod-autoscaler-0.14.0"
# URL_PREFIX="https://raw.githubusercontent.com/kubernetes/autoscaler/${TARGET_GIT_TAG}/vertical-pod-autoscaler/deploy/"
# COMPONENTS="vpa-v1-crd-gen vpa-rbac updater-deployment recommender-deployment admission-controller-deployment"
# For each component, download the yaml, replace the images, and write to disk.
# for component in ${COMPONENTS}; do
#     echo "Downloading ${component}.yaml"
#     wget "${URL_PREFIX}${component}.yaml" -q -O - \
#         | sed -e "s,${DEFAULT_REGISTRY}/\([a-z-]*\):.*,${TARGET_REGISTRY}/\1:${TARGET_IMAGE_TAG}," \
#         > "${component}.yaml"
# done

# Download and render the template yaml, replace the images, and write to disk.
# Installing from YAML source requires generating and signing a TLS cert, which
# is much easier to do with helm, but the upstream repo authors don't want to
# maintain a helm chart. So we're using a third-party one here, for testing.
# https://artifacthub.io/packages/helm/cowboysysop/vertical-pod-autoscaler
helm template "test" \
    --namespace "default" \
    --set ".Release.Namespace=default" \
    --repo https://cowboysysop.github.io/charts \
    --version "7.2.0" \
    vertical-pod-autoscaler \
    | sed -e "s,${DEFAULT_REGISTRY}/\([a-z-]*\):.*,${TARGET_REGISTRY}/\1:${TARGET_IMAGE_TAG}," \
    > components.yaml

# Set namespace (because helm template won't do it...)
kpt fn eval components.yaml -i set-namespace:v0.4.1 -- namespace=default
