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

####################################################################################################
# FUNCTION DECLARATIONS
#
# It's crucial that error messages in these functions be sent to stderr with 1>&2.
# Otherwise, they will not bubble up to the make target that calls this script.

latest_rc_of_version () {
  local version="$1"
  if [ -z "$version" ]; then
    echo "Error: function expects version argument" 1>&2
    exit 1
  fi


  # 1. Get all the versions with the right prefix,
  # 2. order them by their "rc.X" values
  # 3. Take the last/highest one
  rc=$(git tag --list "$version*" | sort -V | tail -n 1)
  if [ -z "$rc" ]; then
    echo "Error: no RC found with prefix: ${version}" 1>&2
    exit 1
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
    echo "Error: must pass an argument in SEMVER style.  Looks like vMAJOR.MINOR.PATCH-rc.X" 1>&2
    exit 1
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

if [ -z "$CS_VERSION" ]; then
  echo "Error: must specify enviroment variable CS_VERSION in style vMAJOR.MINOR.PATCH" 1>&2
  exit 1
fi

# If a user omits the leading "v" (enters 1.2.3 instead of v1.2.3), handle it.
if ! [[ "${CS_VERSION}" = v* ]]; then
  CS_VERSION="v${CS_VERSION}"
fi

# Fetch all existing tags
git fetch --tags > /dev/null
echo "+++ Successfully fetched tags"

RC=$(latest_rc_of_version "$CS_VERSION")
echo "+++ Most recent RC of version $CS_VERSION: $RC"

NEXT_RC=$(increment_rc_of_semver "$RC")
echo "+++ Incremented RC.  NEXT_RC: $NEXT_RC"

git tag -a "$NEXT_RC" -m "Release candidate $NEXT_RC"
echo "+++ Succesfully tagged commit $(git rev-parse HEAD) as $NEXT_RC"
