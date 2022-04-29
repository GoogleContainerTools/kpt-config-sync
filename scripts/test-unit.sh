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

export PATH="${PATH}:./third_party/bats-core/libexec:/go/bin"

echo "Running go tests:"
go test -i -installsuffix "static" "$@"
go test -installsuffix "static" "$@"

# "none" is just a dummy value so nomoserrors doesn't actually print out errors.
# "none" isn't a special value; any non-numeric non-empty string will do.
go run cmd/nomoserrors/main.go --id=none

echo "PASS"

echo
