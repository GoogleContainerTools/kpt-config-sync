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


echo "+++ Building build/test-e2e-go/kind/Dockerfile prow-image"
docker build . -f build/test-e2e-go/kind/Dockerfile -t prow-image
# The .sock volume allows you to connect to the Docker daemon of the host.
# Part of the docker-in-docker pattern.

echo "+++ Running go e2e tests with" "$@"
docker run \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v "$ARTIFACTS":/logs/artifacts \
  --network="host" prow-image \
  ./build/test-e2e-go/e2e.sh "$@"
