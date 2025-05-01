# Copyright 2025 Google LLC
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

# Global build arguments - these can be overridden with --build-arg
ARG GOOS=linux
ARG GOARCH

# Build stage
ARG GOLANG_IMAGE=golang:1.24

FROM --platform=${BUILDPLATFORM} ${GOLANG_IMAGE} AS builder

# Create and set working directory
WORKDIR /workspace

# Copy go.mod and go.sum first for better caching
COPY go.mod go.sum ./
RUN go mod tidy && \
    go mod vendor

# Copy all source files
COPY . .

# Build the binary
RUN if [ -z "${GOARCH}" ]; then \
        GOARCH=$(go env GOARCH); \
    fi
RUN mkdir -p .output/go/bin/${GOARCH} && \
    CGO_ENABLED=0 GOOS=${GOOS} GOARCH=${GOARCH} go build -o .output/go/bin/${GOARCH}/sync-status-watch .

# Final stage
FROM gcr.io/distroless/static:nonroot

# Re-declare build arguments - inherits value from global ARG if set
ARG GOARCH

# Create and set working directory
WORKDIR /app

# Copy the built binary from the builder stage
COPY --from=builder /workspace/.output/go/bin/${GOARCH}/sync-status-watch /app/sync-status-watch

# Expose health check port
EXPOSE 8081

# Use an unprivileged user
USER nonroot:nonroot

CMD ["/app/sync-status-watch"]
