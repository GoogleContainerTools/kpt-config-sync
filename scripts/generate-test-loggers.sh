#!/bin/bash
# Copyright 2023 Google LLC
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

GO_MODULE="$(grep "^module" "go.mod" | cut -d' ' -f2)"

source_paths=("pkg" "cmd")

function render_main_test() {
  cat << EOF
package ${PACKAGE_NAME}

import (
	"os"
	"testing"

	"k8s.io/klog/v2"
)

// TestMain executes the tests for this package, with optional logging.
// To see all logs, use:
// go test ${GO_MODULE}/${PACKAGE_PATH} -v -args -v=5
func TestMain(m *testing.M) {
	klog.InitFlags(nil)
	os.Exit(m.Run())
}
EOF
}

# find_test_dirs loops through all the directories under the specified path
# and prints the ones directly containing go tests.
function find_test_dirs() {
    local parent_path="$1"
    local test_dir_path
    declare -A test_dir_paths
    while IFS= read -r file_path; do
        test_dir_path="$(dirname "${file_path}")"
        test_dir_paths[$test_dir_path]="1"
    done <<< "$(find "${parent_path}" -type f -name "*_test.go")"
    for test_dir_path in "${!test_dir_paths[@]}"; do
        echo "${test_dir_path}"
    done
}

for source_path in "${source_paths[@]}"; do
  while IFS= read -r test_dir_path; do
    file_name="${test_dir_path}/main_test.go"
    echo "Generating ${file_name}"
    PACKAGE_NAME="$(basename "${test_dir_path}")"
    PACKAGE_PATH="${test_dir_path}"
    render_main_test > "${file_name}"
    "addlicense" -c "Google LLC" -f LICENSE_TEMPLATE "${file_name}"
  done <<< "$(find_test_dirs "${source_path}")"
done
