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

# Setup

REPO_DIR="$(readlink -f "$(dirname "$0")")/.."

if [ "${GCP_PROJECT:-"unset"}" == "unset" ]; then
  if ! [ -x "$(command -v gcloud)" ]; then
    echo 'Error: gcloud is not available. Please provide the GCP_PROJECT env variable to specify your GCP project' >&2
    exit 1
  else
    GCP_PROJECT=$(gcloud config get-value project)
  fi
fi

TAG="${TAG:-"latest"}"

echo "+++ Building and pushing images"
make -C "${REPO_DIR}" oss-manifests oss-push-images GCP_PROJECT="${GCP_PROJECT}" TAG="${TAG}"
