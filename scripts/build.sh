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

set -euo pipefail

PLATFORMS=()
VERSION=""
while [[ $# -gt 0 ]]; do
  ARG="${1:-}"
  shift
  case ${ARG} in
    --version)
      VERSION=${1:-}
      shift
      ;;
    *)
      PLATFORMS+=("$ARG")
    ;;
  esac
done

if [[ -z "${VERSION}" ]]; then
    echo "VERSION must be set"
    exit 1
fi

for PLATFORM in "${PLATFORMS[@]}"; do
  # Example: "linux_amd64" -> ["linux" "amd64"]
  IFS='_' read -r -a platform_split <<< "${PLATFORM}"
  GOOS="${platform_split[0]}"
  GOARCH="${platform_split[1]}"

  echo "Building nomos for ${PLATFORM}"
  env GOOS="${GOOS}" GOARCH="${GOARCH}" CGO_ENABLED="0" go install \
      -installsuffix "static" \
      -ldflags "-X kpt.dev/configsync/pkg/version.VERSION=${VERSION}" \
      ./cmd/nomos

  # When go builds for native architecture, it puts output in $GOPATH/bin
  # but when cross compiling, it puts it in the $GOPATH/bin/$GOOS_$GOARCH dir
  if [ -f "${GOPATH}/bin/nomos" ]; then
    mv "${GOPATH}/bin/nomos" "${GOPATH}/bin/${PLATFORM}/nomos"
  fi

  bin=nomos
  if [[ "${GOOS}" == "windows" ]]; then
    bin="$bin.exe"
  fi
  # Use upx to reduce binary size.
  if [[ "${GOOS}" != "darwin" ]]; then
    # upx doesn't play nice with Darwin 16 for Go Binaries.
    # We don't care to debug this since the file size difference is ~20MB.
    upx "${GOPATH}/bin/${PLATFORM}/${bin}"
  fi
done
