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

# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# *** WARNING ***
# This image is built once and reused on repeated test runs.  If you make a
# change here, you *must* rebuild the image and push it to the Nomos container
# registry so that new test runs see the updated image.  This is not ideal but
# we don't have a better approach at the moment.
#
# To rebuild and push the image, run:
#     make push-e2e-tests

# Build intermediate image for tools
# When adding helper binaries, ensure to build from a specific commit ID, rather
# than a tag or a branch, so that rebuilds are reproducible. See examples below
# on how.
FROM golang:1.17.7-alpine AS build-base
RUN apk add bash coreutils git gcc g++ libc-dev unzip

ENV GO111MODULE=on
# Build static binaries only.
ENV CGO_ENABLED=0

# tap2junit converts TAP testing output to jUnit
# v0.0.4
RUN go install github.com/filmil/tap2junit/cmd/tap2junit@afa095c33668ed9abcaeaee5cf0f27f67e96908c

# gotopt2 parses command line options
# v0.1.2
RUN go install github.com/filmil/gotopt2/cmd/gotopt2@v0.1.2

# kind can run kubernetes clusters in docker
# v0.10.0
RUN go install sigs.k8s.io/kind@v0.10.0

# Build intermediate image for gcloud / kubectl
FROM marketplace.gcr.io/google/ubuntu1804 as gcloud-install

ENV PATH /opt/gcloud/google-cloud-sdk/bin:$PATH
RUN apt-get update && apt-get install -y curl python
ARG DL_GOOGLE_COM="https://dl.google.com/dl/cloudsdk/channels/rapid/downloads"
RUN curl ${DL_GOOGLE_COM}/google-cloud-sdk-255.0.0-linux-x86_64.tar.gz \
	> /tmp/google-cloud-sdk.tar.gz
# Verify SHA256 sum for the downloaded archive for reproducibility.
RUN echo "18fcbc81b3b095ff5ef92fd41286a045f782c18d99a976c0621140a8fde3fbad /tmp/google-cloud-sdk.tar.gz" | \
		sha256sum --check -
RUN mkdir -p /opt/gcloud && \
    tar -C /opt/gcloud -xf /tmp/google-cloud-sdk.tar.gz && \
    /opt/gcloud/google-cloud-sdk/install.sh --quiet && \
    gcloud components install alpha beta

# Build e2e image
FROM marketplace.gcr.io/google/ubuntu1804 as e2e-base

ENV DEBIAN_FRONTEND=noninteractive

# Install system stuff
RUN apt-get update \
  && apt-get upgrade -y \
  && apt-get install -y \
  apt-transport-https \
  apt-utils \
  curl \
  git \
  jq \
  libtap-formatter-junit-perl \
  net-tools \
  python \
  software-properties-common \
  unzip

# Add Tini, note that the gpg variant of pulling tini is problematic on prow.
ENV TINI_VERSION v0.19.0
#ADD https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini /tini
#ADD https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini.asc /tini.asc
#RUN gpg --batch --keyserver hkp://p80.pool.sks-keyservers.net:80 --recv-keys 595E85A6B1B4779EA4DAAEC70B588DFF0527A9B7 \
# && gpg --batch --verify /tini.asc /tini
RUN curl -L https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini > /tini \
  && echo "93dcc18adc78c65a028a84799ecf8ad40c936fdfc5f2a57b1acda5a8117fa82c  /tini" | sha256sum --check \
  && chmod +x /tini

ARG UNAME
ARG UID
ARG GID

# The existence of a valid UID, GID and UNAME (USER) is required for programs
# that expect a "regular" unix environment, for example 'git'.
RUN bash -c 'echo UID=${UID}; GID=${GID}; UNAME=${UNAME}'

# This is needed to get build going on any environment with a nonprivileged
# user.  Turns out that on go/prow, the build is ran as root, so no need for
# any such setup.  Hence is done conditionally.
RUN bash -c '[ $GID == 0 ] || groupadd -or -g $GID grp '
RUN bash -c '[ $UID == 0 ] || useradd -u $UID -G $GID $UNAME'

# Install a specific version of kubectl
RUN curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | apt-key add - \
  && echo "deb http://apt.kubernetes.io/ kubernetes-xenial main" > /etc/apt/sources.list.d/kubernetes.list \
  && apt-get update \
  && apt-get install -y kubectl=1.20.4-00

# Copy installed gcloud into image
COPY --from=gcloud-install /opt/gcloud/google-cloud-sdk /opt/gcloud/google-cloud-sdk
ENV PATH /opt/gcloud/google-cloud-sdk/bin:$PATH

# Copy the filmil stuff (gotopt2 and tap2junit) into /opt/bin then add it to
# the PATH env var
COPY --from=build-base /go/bin /opt/bin
ENV PATH /opt/bin:$PATH

ENV GIT_CONFIG_NOGLOBAL 1
ENV GIT_CONFIG_NOSYSTEM 1

# The e2e.sh launcher mounts $REPO/.output/go at /opt/testing/go.  Add the
# mounted go binary output dir to the PATH env var to expose the nomos command
# to tests so they don't have to use the absolute path to the binary.
ENV PATH /opt/testing/go/bin/linux_amd64:$PATH

ENV USER $UNAME
USER $UNAME

WORKDIR /opt/testing/nomos

# tini (especially with -g) does correct handling of signals (like Ctrl+C) that you don't
# get by running a shell script as the entrypoint. If this were removed, Ctrl+C wouldn't
# work.
ENTRYPOINT ["/tini", "-g", "--"]
