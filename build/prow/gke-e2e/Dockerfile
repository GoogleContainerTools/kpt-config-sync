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

# Build intermediate image for gcloud / kubectl
FROM gcr.io/google.com/cloudsdktool/cloud-sdk:425.0.0-slim as gcloud-install
RUN apt-get install -y kubectl google-cloud-sdk-gke-gcloud-auth-plugin

FROM golang:1.20-alpine as builder

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
FROM golang:1.20-alpine as kpt-config-sync-e2e

# Since go modules isn't enabled by default.
ENV GO111MODULE=on
# Build static binaries; otherwise go test complains.
ENV CGO_ENABLED=0

RUN apk add --no-cache \
  bash curl docker gcc git jq make openssh-client python3 diffutils

# Copy installed gcloud and kubectl into image
COPY --from=gcloud-install /usr/lib/google-cloud-sdk /opt/gcloud/google-cloud-sdk
COPY --from=gcloud-install /usr/bin/kubectl /opt/gcloud/google-cloud-sdk/bin/kubectl
COPY --from=builder /go/bin/junit-report /go/bin/junit-report
COPY --from=builder /go/bin/go-junit-report /go/bin/go-junit-report
ENV PATH /opt/gcloud/google-cloud-sdk/bin:$PATH
