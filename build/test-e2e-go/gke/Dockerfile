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
FROM gcr.io/google.com/cloudsdktool/cloud-sdk:369.0.0-slim as gcloud-install
RUN apt-get install -y kubectl

# Build e2e image
FROM golang:1.17.7-alpine

WORKDIR /repo

# Since go modules isn't enabled by default.
ENV GO111MODULE=on
# Build static binaries; otherwise go test complains.
ENV CGO_ENABLED=0
# Set Kubernetes environment.
ENV KUBERNETES_ENV=GKE

RUN apk add bash curl docker gcc git jq make openssh-client python3 diffutils

ARG HELM_VERSION=v3.6.3
ARG KUSTOMIZE_VERSION=v4.5.2
ARG HELM_INFLATOR_FUNCTIOPN_VERSION=v0.1.0

# Install the render-helm-chart function.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on \
  go install github.com/GoogleContainerTools/kpt-functions-catalog/functions/go/render-helm-chart@${HELM_INFLATOR_FUNCTIOPN_VERSION}

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
RUN go install github.com/jstemmer/go-junit-report@v1.0.0

# Steps after here can't be cached since they touch the local filesystem.

# Just copy everything in the nomos repository so we have whatever we might need.
COPY . .

# Make sure the nomos command is available for tests.
RUN go install kpt.dev/configsync/cmd/nomos

# We want bats to be in PATH rather than having to alias it.
COPY third_party/bats-core/bin/bats /bin
