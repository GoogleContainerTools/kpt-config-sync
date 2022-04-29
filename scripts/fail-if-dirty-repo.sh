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


set -eou pipefail

is_dirty_repo() {
  [[ -n "$(git status --short)" ]]
  return "$?"
}

if is_dirty_repo; then
  echo "! Error: Cannot push to commit/ from dirty repo"
  echo "Git status output:"
  # Prefix git status output with a pound sign for better readability
  git status | sed 's/.*/# &/'
  exit 1
fi
