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


echo "+++ Running Mono Repo tests"
GO111MODULE=on go test ./e2e/... --e2e "$@"
mono_repo_exit=$?

echo "+++ Running Multi Repo tests"
GO111MODULE=on go test ./e2e/... --e2e --multirepo "$@"
multi_repo_exit=$?

if (( mono_repo_exit != 0 || multi_repo_exit != 0 )); then
  exit 1
fi
