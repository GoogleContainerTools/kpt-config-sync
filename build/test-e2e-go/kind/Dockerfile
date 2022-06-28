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

FROM golang:1.17.7-alpine

WORKDIR /repo

# Since go modules isn't enabled by default.
ENV GO111MODULE=on
# Build static binaries; otherwise go test complains.
ENV CGO_ENABLED=0
# Set Kubernetes environment.
ENV KUBERNETES_ENV=KIND

# We're running tests within kind v0.11.1 clusters.
# If you upgrade the version, you also need to update the special kubekins-with-Kind
# images we use in the test-infra repository. See Makefile.prow.
RUN go install sigs.k8s.io/kind@v0.11.1

RUN apk add --no-cache \
  bash curl docker gcc git jq make openssh-client diffutils

# Install kubectl and make sure it's available in the PATH.
RUN curl -LO https://storage.googleapis.com/kubernetes-release/release/`curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt`/bin/linux/amd64/kubectl
RUN chmod +x ./kubectl
RUN mv ./kubectl /bin

ARG HELM_VERSION=v3.6.3
ARG KUSTOMIZE_VERSION=v4.5.2
ARG HELM_INFLATOR_FUNCTIOPN_VERSION=v0.2.0

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

# Get go-junit-report.
RUN go install github.com/jstemmer/go-junit-report@v1.0.0

# Steps after here can't be cached since they touch the local filesystem.

# Just copy everything in the nomos repository so we have whatever we might need.
COPY . .

# Make sure the nomos command is available for tests.
RUN go install kpt.dev/configsync/cmd/nomos

# We want bats to be in PATH rather than having to alias it.
COPY third_party/bats-core/bin/bats /bin
