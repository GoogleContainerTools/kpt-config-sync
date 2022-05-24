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

# Pre-requisite:
#
# Gcloud: stolos-dev.
# kubectl: user cluster in Stolos-dev.

# Setup

REPO_DIR="$(readlink -f "$(dirname "$0")")/.."

WORK_DIR=$(mktemp -d -p "$REPO_DIR")

function cleanup {
  rm -rf "$WORK_DIR"
  echo "+++ Deleted temp working directory $WORK_DIR"
}

trap cleanup EXIT

# Invoking the build-oss.sh script to build the images and generate the manifests.
MANIFEST_DIR="${WORK_DIR}" "${REPO_DIR}"/scripts/build-oss.sh

# Apply the manifests to the cluster.
echo "+++ Applying manifests"
kubectl apply -f "${WORK_DIR}"

# Cleanup exiting root-ssh-key secret.
kubectl delete secret root-ssh-key -n=config-management-system --ignore-not-found
# Create root-ssh-key secret for Root Reconciler.
# shellcheck disable=SC2086
kubectl create secret generic root-ssh-key -n=config-management-system \
      --from-file=ssh=${HOME}/.ssh/id_rsa.nomos

# Verify reconciler-manager pod is running.

# Apply RootSync CR.
kubectl apply -f e2e/testdata/reconciler-manager/rootsync-sample.yaml

# Apply RepoSync CR.
kubectl apply -f e2e/testdata/reconciler-manager/reposync-sample.yaml

sleep 10s

# Verify whether respective controllers create various objects i.e. Deployments.
kubectl get all -n config-management-system

# Verify whether config map is created.
kubectl get cm -n config-management-system

# End
