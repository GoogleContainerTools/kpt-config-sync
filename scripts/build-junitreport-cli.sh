#!/bin/bash
#
# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euox pipefail

GOPATH="${GOPATH:-$(go env GOPATH)}"

PLATFORM=$1
VERSION=$2

if [[ -z "${VERSION}" ]]; then
    echo "VERSION must be set"
    exit 1
fi

# Example: "linux_amd64" -> ["linux" "amd64"]
IFS='_' read -r -a platform_split <<< "${PLATFORM}"
GOOS="${platform_split[0]}"
GOARCH="${platform_split[1]}"

echo "Building junit-report for ${PLATFORM}"
env GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED="0" go install \
    -installsuffix "static" \
    -ldflags "-X kpt.dev/configsync/pkg/version.VERSION=${VERSION}" \
    ./cmd/junit-report

# When go builds for native architecture, it puts output in $GOPATH/bin
# but when cross compiling, it puts it in the $GOPATH/bin/$GOOS_$GOARCH dir
if [ -f "${GOPATH}/bin/junit-report" ]; then
  mv "${GOPATH}/bin/junit-report" "${GOPATH}/bin/${PLATFORM}/junit-report"
fi
