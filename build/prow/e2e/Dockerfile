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


ARG GCLOUD_IMAGE
ARG GOLANG_IMAGE
ARG DOCKER_CLI_IMAGE

# Build intermediate image for gcloud / kubectl
FROM ${GCLOUD_IMAGE} as gcloud-install

RUN apt-get update && apt-get install -y \
    kubectl google-cloud-sdk-gke-gcloud-auth-plugin

FROM ${GOLANG_IMAGE} as builder

WORKDIR /workspace

COPY . .

# Since go modules isn't enabled by default.
ENV GO111MODULE=on
# Build static binaries; otherwise go test complains.
ENV CGO_ENABLED=0

# Make sure the junit-report command is available for tests.
RUN go install ./cmd/junit-report
# Get go-junit-report
RUN go install github.com/jstemmer/go-junit-report/v2@v2.0.0

# Build e2e image
FROM ${DOCKER_CLI_IMAGE} as gke-e2e

WORKDIR /workspace

ENV GOPATH=/go
# Since go modules isn't enabled by default.
ENV GO111MODULE=on
# Build static binaries; otherwise go test complains.
ENV CGO_ENABLED=0

RUN apt-get update && apt-get install -y \
  bash curl gcc git jq make openssh-client python3 diffutils

# Copy installed gcloud and kubectl into image
COPY --from=gcloud-install /usr/lib/google-cloud-sdk /opt/gcloud/google-cloud-sdk
COPY --from=gcloud-install /usr/bin/kubectl /usr/bin/kubectl
# Copy golang and built binaries into image
COPY --from=builder /usr/local/go /usr/local/go
COPY --from=builder /go /go
COPY --from=builder /go/bin/junit-report /go/bin/junit-report
COPY --from=builder /go/bin/go-junit-report /go/bin/go-junit-report
ENV PATH /go/bin:/usr/local/go/bin:/opt/gcloud/google-cloud-sdk/bin:$PATH

ENTRYPOINT ["/usr/bin/bash"]

# KinD-specific image with latest config-sync code
FROM gke-e2e as kind-e2e

WORKDIR /repo
# Steps after here can't be cached since they touch the local filesystem.
# Just copy everything in the nomos repository so we have whatever we might need.
COPY . .

# install the nomos CLI, which is used in several e2e tests.
RUN go install ./cmd/nomos

# add the binaries in the local build directory to PATH (e.g. helm, kustomize, kind)
ENV PATH /repo/.output/go/bin:$PATH

# Set Kubernetes environment.
ENV KUBERNETES_ENV=KIND