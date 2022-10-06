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


# The purpose of this script is to promote a git commit, based on a vMAJOR.MINOR.PATCH-rc.N
# release candidate passed in as an environment variable. The commit will be tagged
# using vMAJOR.MINOR.PATCH

set -eo pipefail
set +o history

####################################################################################################
# FUNCTION DECLARATIONS
#
# It's crucial that error messages in these functions be sent to stderr with 1>&2.
# Otherwise, they will not bubble up to the make target that calls this script.

err () {
  local msg="$1"
  echo "${msg}" 1>&2
  exit 1
}

####################################################################################################

if ! [[ "${RC_TAG}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-rc\.[0-9]+$ ]]; then
  err "Error: RC_TAG must be in the form v1.2.3-rc.4 - got ${RC_TAG}"
fi

CS_VERSION="${RC_TAG%-rc.*}"

read -rp "${RC_TAG} will be promoted to ${CS_VERSION} - Proceed (yes/no)? " choice
case "${choice}" in
  yes) ;;
  no) exit 1 ;;
  *) err "Unrecognized choice ${choice}"
esac

REMOTE="git@github.com:GoogleContainerTools/kpt-config-sync.git"
# Fetch all existing tags
git fetch "${REMOTE}" "${RC_TAG}" --tags > /dev/null
echo "+++ Successfully fetched tags"

echo "+++ Promoting ${RC_TAG} to ${CS_VERSION}"
git tag -a "${CS_VERSION}" -m "Release ${CS_VERSION}" "${RC_TAG}"^{}

echo "+++ Pushing tag ${CS_VERSION}"
git push "${REMOTE}" "${CS_VERSION}"
