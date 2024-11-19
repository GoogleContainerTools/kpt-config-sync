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

ARG GOLANG_IMAGE
ARG DEBIAN_BASE_IMAGE

# Build all Config Sync go binaries
FROM ${GOLANG_IMAGE} as bins

WORKDIR /workspace

COPY . .

# Version string to embed in built binary.
ARG VERSION

# Build all our stuff.
RUN CGO_ENABLED=0 GOOS=linux GO111MODULE=on \
  go install \
    -mod=vendor \
    -ldflags "-X kpt.dev/configsync/pkg/version.VERSION=${VERSION}" \
    ./cmd/nomos \
    ./cmd/reconciler \
    ./cmd/reconciler-manager \
    ./cmd/hydration-controller \
    ./cmd/admission-webhook \
    ./cmd/oci-sync \
    ./cmd/helm-sync \
    ./cmd/gcenode-askpass-sidecar \
    ./cmd/resource-group


# Concatenate vendored licenses into LICENSES.txt
# Built in the container to include binary licenses (helm & kustomize)
RUN scripts/prepare-licenses.sh

# Debian non-root base image
# Uses the same nonroot UID as distroless
FROM ${DEBIAN_BASE_IMAGE} as debian-nonroot
WORKDIR /
ARG USERNAME=nonroot
ARG USER_UID=65532
ARG USER_GID=$USER_UID
RUN groupadd --gid $USER_GID $USERNAME && \
  useradd --uid $USER_UID --gid $USER_GID -m $USERNAME
RUN apt-get update && apt-get install -y bash git
USER nonroot:nonroot

# Hydration controller image
FROM gcr.io/distroless/static:nonroot as hydration-controller
WORKDIR /
COPY --from=bins /go/bin/hydration-controller .
COPY --from=bins /workspace/.output/third_party/helm/helm /usr/local/bin/helm
COPY --from=bins /workspace/.output/third_party/helm/NOTICES /third_party/helm/NOTICES
COPY --from=bins /workspace/.output/third_party/kustomize/kustomize /usr/local/bin/kustomize
COPY --from=bins /workspace/.output/third_party/kustomize/NOTICES /third_party/kustomize/NOTICES
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/hydration-controller"]

# OCI-sync image
FROM gcr.io/distroless/static:latest as oci-sync
# Setting HOME ensures that whatever UID this ultimately runs as can write files.
ENV HOME=/tmp
WORKDIR /
COPY --from=bins /go/bin/oci-sync .
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/oci-sync"]

# Helm-sync image
FROM gcr.io/distroless/static:latest as helm-sync
# Setting HOME ensures that whatever UID this ultimately runs as can write files.
ENV HOME=/tmp
WORKDIR /
COPY --from=bins /go/bin/helm-sync .
COPY --from=bins /workspace/.output/third_party/helm/helm /usr/local/bin/helm
COPY --from=bins /workspace/.output/third_party/helm/NOTICES /third_party/helm/NOTICES
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/helm-sync"]

# Hydration controller image with shell
FROM debian-nonroot as hydration-controller-with-shell
WORKDIR /
USER root
COPY --from=bins /go/bin/hydration-controller .
COPY --from=bins /workspace/.output/third_party/helm/helm /usr/local/bin/helm
COPY --from=bins /workspace/.output/third_party/helm/NOTICES /third_party/helm/NOTICES
COPY --from=bins /workspace/.output/third_party/kustomize/kustomize /usr/local/bin/kustomize
COPY --from=bins /workspace/.output/third_party/kustomize/NOTICES /third_party/kustomize/NOTICES
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/hydration-controller"]

# Reconciler image
FROM gcr.io/distroless/static:nonroot as reconciler
WORKDIR /
COPY --from=bins /go/bin/reconciler .
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/reconciler"]

# Reconciler Manager image
FROM gcr.io/distroless/static:nonroot as reconciler-manager
WORKDIR /
COPY --from=bins /go/bin/reconciler-manager reconciler-manager
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/reconciler-manager"]

# Admission Webhook image
FROM gcr.io/distroless/static:nonroot as admission-webhook
WORKDIR /
COPY --from=bins /go/bin/admission-webhook admission-webhook
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/admission-webhook"]

# Askpass image
FROM gcr.io/distroless/static:nonroot as gcenode-askpass-sidecar
WORKDIR /
COPY --from=bins /go/bin/gcenode-askpass-sidecar gcenode-askpass-sidecar
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/gcenode-askpass-sidecar"]

# Resource group controller image
FROM gcr.io/distroless/static:nonroot as resource-group-controller
WORKDIR /
COPY --from=bins /go/bin/resource-group resource-group
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt
USER nonroot:nonroot
ENTRYPOINT ["/resource-group"]

# Nomos image
# Not used by Config Sync backend components. Intended for use cases with the
# nomos CLI (e.g. containerized CI/CD)
FROM debian-nonroot as nomos
USER root

# Install nomos CLI
RUN mkdir -p /opt/nomos/bin
WORKDIR /opt/nomos/bin
COPY --from=bins /go/bin/nomos nomos
COPY --from=bins /workspace/.output/third_party/helm/helm /usr/local/bin/helm
COPY --from=bins /workspace/.output/third_party/helm/NOTICES /third_party/helm/NOTICES
COPY --from=bins /workspace/.output/third_party/kustomize/kustomize /usr/local/bin/kustomize
COPY --from=bins /workspace/.output/third_party/kustomize/NOTICES /third_party/kustomize/NOTICES
COPY --from=bins /workspace/LICENSE LICENSE
COPY --from=bins /workspace/LICENSES.txt LICENSES.txt

# Set up a HOME directory for non-root user
RUN mkdir -p /nomos && chown nonroot:nonroot /nomos
USER nonroot:nonroot
ENV HOME="/nomos"
ENV PATH="/opt/nomos/bin:${PATH}"
ENTRYPOINT ["/opt/nomos/bin/nomos"]
