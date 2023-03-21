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

export CGO_ENABLED=0

# TODO: It is best practice to install directly on the Docker image,
#  but for now it's unclear how to do this sanely.
wget -O- -nv https://raw.githubusercontent.com/golangci/golangci-lint/v1.52.0/install.sh | \
  sh -sx -- -b .output/go/bin v1.52.0

# golangci-lint uses $HOME to determine where to store .cache information.
# For the docker image this is running in, $HOME is set to "/", so for this
# script we overwrite that as the docker image does not have permission to
# create a /.cache directory.
HOME=.output/

echo "Running golangci-lint: "
if ! OUT="$(.output/go/bin/golangci-lint run --exclude-use-default=false)"; then
  echo "${OUT}"

  NC=''
  RED=''
  if [[ -t 1 ]]; then
    NC='\033[0m'
    RED='\033[0;31m'
  fi

  # make fmt-go only resolves gofmt and goimports errors, since the other linters
  # don't have autoformatters.
  if echo "${OUT}" | grep "(gofmt)" > /dev/null; then
    echo -e "${RED}ADVICE${NC}: running \"make fmt-go\" may fix the (gofmt) error"
  fi
  if echo "${OUT}" | grep "(goimports)" > /dev/null; then
    echo -e "${RED}ADVICE${NC}: running \"make fmt-go\" may fix the (goimports) error"
  fi
  exit 1
fi
echo "PASS"
echo
