#!/bin/bash

# Check that only one ConfigMap exists with a name starting with the prefix.
#
# Arguments:
#   nameprefix: the ConfigMap name prefix
#   namespace: the namespace the ConfigMap exists in
function configmaps::check_one_exists() {
  local nameprefix="${1:-}"
  local namespace="${2:-}"
  configMapCount=$(kubectl get configmaps -n "${namespace}" -o json | jq --arg prefix "$nameprefix" ".items | to_entries | .[] | .value.metadata.name | select(startswith(\"$nameprefix\"))" | wc -l)
  if (( configMapCount != 1 )); then
    echo "ConfigMap count with prefix ${nameprefix} is ${configMapCount}, not 1"
    return 1
  fi
}
