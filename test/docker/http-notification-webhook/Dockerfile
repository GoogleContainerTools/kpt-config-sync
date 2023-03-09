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

FROM golang:1.19 as bins

WORKDIR /workspace

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on \
  go install \
    ./test/docker/http-notification-webhook

# notification-webhook image
FROM gcr.io/distroless/static:nonroot as notification-webhook
WORKDIR /
COPY --from=bins /go/bin/http-notification-webhook .

# Switch to non-root user
USER 1000

ENTRYPOINT ["/http-notification-webhook"]
