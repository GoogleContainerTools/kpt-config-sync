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


# Helper script to get our docker tag if we're checked out on a branch
# e.g. 1.4.0.  If we're on master, or if this is a local branch, and we use tag
# "latest".
#
# This lets us stop overwriting gcr.io/config-management-release/nomos:latest
# with images built on branches.
#
# Additional backstory in b/159654901
#
# Get the upstream tracking branch.  Note that this needs to work in multiple
# checkout/fetch environments.  See b/159654901 for the various changes that we
# tried before settling on this.
BRANCH=$(git symbolic-ref --short --quiet HEAD);
if [[ "${BRANCH}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "${BRANCH}-latest"
else
  echo "latest"
fi
