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

# Build intermediate image for gcloud / kubectl
FROM gcr.io/google.com/cloudsdktool/cloud-sdk:425.0.0-slim as gcloud-install
RUN apt-get install -y kubectl google-cloud-sdk-gke-gcloud-auth-plugin

# Build e2e image
FROM golang:1.19-alpine as kpt-config-sync-e2e

WORKDIR /repo

# Since go modules isn't enabled by default.
ENV GO111MODULE=on
# Build static binaries; otherwise go test complains.
ENV CGO_ENABLED=0

RUN apk add --no-cache \
  bash curl docker gcc git jq make openssh-client python3 diffutils

ARG HELM_VERSION=v3.12.2
ARG KUSTOMIZE_VERSION=v5.1.0

# Install Helm
RUN wget https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz -O /tmp/helm-${HELM_VERSION}-linux-amd64.tar.gz && \
  tar -zxvf /tmp/helm-${HELM_VERSION}-linux-amd64.tar.gz -C /tmp && \
  mv /tmp/linux-amd64/helm /usr/local/bin/helm && \
  rm -rf /tmp/linux-amd64 /tmp/helm-${HELM_VERSION}-linux-amd64.tar.gz

# Install Kustomize
RUN wget https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize%2F${KUSTOMIZE_VERSION}/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz -O /tmp/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz && \
  tar -zxvf /tmp/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz -C /tmp && \
  mv /tmp/kustomize /usr/local/bin/kustomize && \
  rm /tmp/kustomize_${KUSTOMIZE_VERSION}_linux_amd64.tar.gz

# Copy installed gcloud and kubectl into image
COPY --from=gcloud-install /usr/lib/google-cloud-sdk /opt/gcloud/google-cloud-sdk
COPY --from=gcloud-install /usr/bin/kubectl /opt/gcloud/google-cloud-sdk/bin/kubectl
ENV PATH /opt/gcloud/google-cloud-sdk/bin:$PATH

# Get go-junit-report
RUN go install github.com/jstemmer/go-junit-report/v2@v2.0.0

# GKE-specific image with latest config-sync code
FROM kpt-config-sync-e2e

# Steps after here can't be cached since they touch the local filesystem.

# Just copy everything in the nomos repository so we have whatever we might need.
COPY . .

# Make sure the nomos command is available for tests.
RUN go install kpt.dev/configsync/cmd/nomos

# Make sure the junit-report command is available for tests.
RUN go install kpt.dev/configsync/cmd/junit-report

# Set Kubernetes environment.
ENV KUBERNETES_ENV=GKE
