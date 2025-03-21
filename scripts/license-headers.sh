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

if [[ -z "$(which addlicense)" ]]; then
  echo "addlicense not in PATH"
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "${REPO_ROOT}"

ignores=(
  "-ignore=vendor/**"
  "-ignore=e2e/testdata/helm-charts/**"
  "-ignore=e2e/testdata/hydration/**"
  "-ignore=test/kustomization/expected.yaml"
  "-ignore=.output/**"
  "-ignore=e2e/testdata/*.xml"
  "-ignore=.idea/**"
  "-ignore=.vscode/**"
)

case "$1" in
  lint)
    "addlicense" -check "${ignores[@]}" . 2>&1 | sed '/ skipping: / d'
    ;;
  add)
    "addlicense" -v -c "Google LLC" -f LICENSE_TEMPLATE \
      "${ignores[@]}" \
      . 2>&1 | sed '/ skipping: / d'
    ;;
  *)
    echo "Usage: $0 (lint|add)"
    exit 1
    ;;
esac
