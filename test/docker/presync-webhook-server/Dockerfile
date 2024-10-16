# Copyright 2024 Google LLC
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

# Build stage
FROM golang:1.23 as builder

WORKDIR /app
COPY . .

RUN go mod init presync-webhook-server || true && go mod tidy

RUN go install github.com/sigstore/cosign/v2/cmd/cosign@v2.4.1

ENV GOPATH=/go
ENV PATH "/go/bin:/usr/local/go/bin:$PATH"

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o webhook-server oci-signature-verification-server.go

# Final stage
FROM gcr.io/google.com/cloudsdktool/google-cloud-cli:slim
WORKDIR /

RUN apt-get update && apt-get install -y google-cloud-sdk-gke-gcloud-auth-plugin

ENV GOPATH=/go
ENV PATH "/go/bin:/usr/local/go/bin:$PATH"

COPY --from=builder /app/webhook-server /webhook-server
COPY --from=builder /go/bin/cosign /usr/local/bin/cosign

RUN chmod +x /webhook-server
RUN chmod +x /usr/local/bin/cosign

CMD ["/webhook-server"]