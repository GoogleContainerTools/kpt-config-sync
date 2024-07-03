#!/bin/bash
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

set -euo pipefail

# Install go-licenses binary from vendored source
go install github.com/google/go-licenses

chmod -R +rwx .output

echo "Ensuring dependencies contain approved licenses. This may take up to 3 minutes."
# Lints licenses for all packages, even ones just for testing.
# Faster than evaluating the dependencies of each individual binary by about 3x.
# We want word-splitting here rather than manually looping through the packages one-by-one.
# shellcheck disable=SC2046
go-licenses check $(go list all)
