#!/bin/bash

# Helpers for getting cluster information

# Checks if the cluster minorVersion is less than a specified value in a 1.x cluster.
#
# Args:
#   minorVersion: The minor version of the K8S cluster we're comparing.
#
cluster::check_minor_version_is_lt() {
  local expected_version=${1}

  local actual_major_version
  actual_major_version=$(kubectl version --output=json | jq -r ".serverVersion.major")
  if (( actual_major_version != 1 ));
  then
    return 1
  fi

  local actual_version_resp
  actual_version_resp=$(kubectl version --output=json | jq -r ".serverVersion.minor")
  local actual_version
  actual_version=${actual_version_resp:0:-1} # Remove '+' suffix
  if (( actual_version < expected_version ));
  then
    return 0
  fi
  return 1
}