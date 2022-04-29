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

#
# NOTE: When updating this file, you will also need to do the following:
#
# 1. Increment the BUILDENV_IMAGE variable in Makefile
# 2. Run the following locally to test the build then push the image so it's
#    accessible by CI:
#    $ make build-buildenv && make build && make push-buildenv
# 3. Upload your change for review and check the CI results.  Note that Prow
#    will fail if you have not uploaded your new image.
#

# TODO: Change the image to be from gcr.io
# when the go1.17 image is released in it.
# Note that we shouldn't use the -alpine image here
# since it is not allowed due to busybox for licensing.
ARG GOLANG_CONTAINER=golang:1.17.7-buster

# Environment to build the helper binaries from.
FROM ${GOLANG_CONTAINER} AS tools-base

# Set GOPATH since it is not already set.
ENV GOPATH=/go

# Update PATH with go specific locations.
ENV PATH "/go/bin:/usr/local/go/bin:$PATH"

RUN apt-get update \
  && apt-get install -y \
  bash \
  git

ENV GO111MODULE=on
# Build static binaries
ENV CGO_ENABLED=0

# Install goimports.
# This is required by the `make fmt-go` target, even though it isn't used
# in building the Nomos image. This was previously included in golang:alpine,
# but not any more so we have to install it ourselves.
RUN go get golang.org/x/tools/cmd/goimports

# gotopt2 parses command line options
# v0.1.2
ARG GOTOPT2_REPO="github.com/filmil/gotopt2"
RUN go get ${GOTOPT2_REPO}/cmd/gotopt2@v0.1.2

# The build environment docker file.
FROM ${GOLANG_CONTAINER}

# Set GOPATH since it is not already set.
ENV GOPATH=/go

# Update PATH with go specific locations.
ENV PATH "/go/bin:/usr/local/go/bin:$PATH"

# Adds support for building dynamically linked libraries.
RUN apt-get update \
  && apt-get install -y \
  bash \
  gcc \
  git \
  musl-dev \
  python-pip \
  upx \
  wget

# Starting from go 1.10, build and test results are cached, which speeds up
# following runs.
ENV GOCACHE=/go/cache

COPY --from=tools-base /go/bin/* /bin/

# To disable an existing ENTRYPOINT, set an empty array. This allows arguments
# to 'docker run' to exec as normal.
ENTRYPOINT []
