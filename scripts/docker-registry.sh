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


set -euxo pipefail

# Starts a local docker registry and connects it to kind.
#
# Required for Go e2e tests to work. See e2e/doc.go.
#
# Installs kind if it is not already installed. If you already have kind
# installed but this does not work, make sure you're on version 0.10.0 or later.

# create registry container unless it already exists
reg_name='kind-registry'
reg_port='5000'
docker inspect "${reg_name}" &>/dev/null || (
  # The container doesn't exist.
  docker run \
    -d --restart=always -p "${reg_port}:5000" --name "${reg_name}" \
    registry:2
)

# The container exists, but might not be running.
# It's safe to run this even if the container is already running.
docker start "${reg_name}"

# Ensure kind v0.11.1 is installed.
# Dear future people: Feel free to upgrade this as new versions are released.
# Note that upgrading the kind version will require updating the image versions:
# https://github.com/kubernetes-sigs/kind/releases
kind &> /dev/null || (
  echo "Kind is not installed. Install v0.11.1."
  echo "https://kind.sigs.k8s.io/docs/user/quick-start/"
  exit 1
)

kind version | grep v0.11.1 || (
  echo "Using unsupported kind version. Install v0.11.1."
  echo "https://kind.sigs.k8s.io/docs/user/quick-start/"
  exit 1
)

# Check if the "kind" docker network exists.
docker network inspect "kind" >/dev/null || (
  # kind doesn't create the docker network until it has been used to create a
  # cluster.
  kind create cluster
  kind delete cluster
)

# Connect the registry to the cluster network if it isn't already.
docker network inspect kind | grep "${reg_name}" || \
  docker network connect "kind" "${reg_name}"
