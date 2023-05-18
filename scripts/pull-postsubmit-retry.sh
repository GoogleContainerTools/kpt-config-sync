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

# The postsubmit job which publishes artifacts takes some time to complete.
# This script retries pulling the postsubmit artifacts for a reasonable amount
# of time and then errors if not found.

set -euo pipefail

# Wait up to 20 minutes for postsubmit artifacts
n=0
num_intervals=80
interval=15
SECONDS=0
until [[ "$n" -ge $num_intervals ]]; do
   make pull-gcs-postsubmit && exit 0
   echo "++++ Failed to pull postsubmit artifacts. Waiting ${interval} seconds to retry."
   n=$((n+1))
   sleep "${interval}"
done

echo "++++ Postsubmit artifacts not found after retrying for ${SECONDS} seconds"
exit 1
