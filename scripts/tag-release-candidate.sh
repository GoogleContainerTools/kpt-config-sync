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


# The purpose of this script is to tag a git commit, based on a vMAJOR.MINOR.PATCH
# prefix, fed in as an environment variable.  Later commands can interact with this
# tag via the git CLI.  This script should be limited to this specific purpose.

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

latest_rc_of_version () {
  local version="$1"
  if [ -z "$version" ]; then
    err "Error: function expects version argument"
  fi


  # 1. Get all the versions with the right prefix,
  # 2. order them by their "rc.X" values
  # 3. Take the last/highest one
  rc=$(git tag --list "${version}-rc.*" | sort -V | tail -n 1)
  if [ -z "$rc" ]; then
    # Return contrived initial zero tag. It will get incremented to 1.
    rc="${version}-rc.0"
    echo "no RC found for ${version} - Creating initial RC" 1>&2
  fi
  echo "$rc"
}

join_by () {
  local IFS="$1" # Set the delimiter character
  shift # Dispose of the first argument, which is the function name
  echo "$*" # Print the remaining arguments, delimited by the selected character
}

increment_rc_of_semver () {
  local semver="${1}"

  if [ -z "$semver" ]; then
    err "Error: must pass an argument in SEMVER style.  Looks like vMAJOR.MINOR.PATCH-rc.X"
  fi

  # Split the semver string on "."
  IFS="." read -r -a semver_tokens <<< "${semver}"

  # Grab the final token, which is the "X" in "rc.X"
  length=${#semver_tokens[@]} # Get the length
  last_index=$((length - 1)) # Decrement
  last_token=${semver_tokens[${last_index}]} # Retrieve the final token

  semver_tokens[last_index]=$((last_token + 1))

  join_by "." "${semver_tokens[@]}"
}

####################################################################################################

# If a user omits the leading "v" (enters 1.2.3 instead of v1.2.3), throw an error.
if ! [[ "${CS_VERSION}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  err "Error: CS_VERSION must be in the form v1.2.3 - got ${CS_VERSION}"
fi

REMOTE="git@github.com:GoogleContainerTools/kpt-config-sync.git"
# Use Github access token if provided
[[ -n "${GH_TOKEN}" ]] && REMOTE="https://${GH_TOKEN}@github.com/GoogleContainerTools/kpt-config-sync.git"
# Fetch all existing tags
git fetch "${REMOTE}" --tags > /dev/null
echo "+++ Successfully fetched tags"

RC=$(latest_rc_of_version "$CS_VERSION")
echo "+++ Most recent RC of version $CS_VERSION: $RC"

NEXT_RC=$(increment_rc_of_semver "$RC")
echo "+++ Incremented RC.  NEXT_RC: $NEXT_RC"

read -rp "This will create ${NEXT_RC} - Proceed (yes/no)? " choice
case "${choice}" in
  yes) ;;
  no) exit 1 ;;
  *) err "Unrecognized choice ${choice}"
esac

git tag -a "$NEXT_RC" -m "Release candidate $NEXT_RC"
echo "+++ Successfully tagged commit $(git rev-parse HEAD) as ${NEXT_RC}"

echo "+++ Pushing RC tag"
git push "${REMOTE}" "${NEXT_RC}"
