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


#
# golang test launcher.
# This wraps the go test execution and creates a junit report.

set -eo pipefail

set +e

test_results=".output/test_results.txt"
echo "Starting tests"
start_time=$(date +%s)
go test --test.v "$@" | tee "${test_results}"
exit_code=$?
end_time=$(date +%s)
echo "Tests took $(( end_time - start_time )) seconds"

# Save test results to ARTIFACTS directory. The ARTIFACTS env var is set by prow.
# The containerized entry points mount the ARTIFACTS directory to a path inside
# the container, and pass the mounted path as ARTIFACTS. Using the env var directly
# enables running this script more flexibly, e.g. without docker in docker.
if [[ -n "${ARTIFACTS}" && -d "${ARTIFACTS}" ]]; then
  echo "Creating junit xml report"
  # Go 1.20 started using "=== NAME" when tests resume instead of "=== CONT".
  # go-junit-report does not yet properly parse "=== NAME", so this hack enables
  # proper parsing.
  # TODO: revert when fixed https://github.com/jstemmer/go-junit-report/issues/169
  sed -i -e 's/=== NAME/=== CONT/g' "${test_results}"
  go-junit-report --subtest-mode=exclude-parents < "${test_results}" > "${ARTIFACTS}/junit_report.xml"
  if [ "$exit_code" -eq 0 ]; then
    # build our in-repo junit report post-processor binary
    go install ./cmd/junit-report
    junit-report reset-failure --path="${ARTIFACTS}/junit_report.xml"
  fi
fi

exit $exit_code
