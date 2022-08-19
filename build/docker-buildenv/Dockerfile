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

ARG BASE_IMAGE=gcr.io/k8s-staging-test-infra/gcb-docker-gcloud:v20220617-174ad91c3a

# Environment to build the helper binaries from.
FROM ${BASE_IMAGE} AS docker-buildenv

RUN apk add rsync

RUN gcloud components install gsutil

