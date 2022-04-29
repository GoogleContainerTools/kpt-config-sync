#!/bin/bash

# Check if a ClusterRole's rules count is a specific amount.
#
# Arguments:
#   name: the ClusterRole name
#   count: the expected rules count
function clusterrole::rules_count_eq() {
  local name="${1:-}"
  local count="${2:-}"
  local actualcount
  actualcount=$(kubectl get clusterrole "${name}" -ojson | jq -c ".rules | length")
  if (( "$count" != "$actualcount" )); then
    echo "clusterroles.rules count is $actualcount waiting for $count"
    return 1
  fi
}

# Check if a ClusterRole is managed and that the selected value is expected.
#
# Arguments:
#   name:  the ClusterRole name
#   sel:   the json path to select the value from
#   value: the expected value at the resource's path
function clusterrole::validate_management_selection() {
  local name="${1:-}"
  local sel="${2:-}"
  local value="${3:-}"
  local resource
  resource=$(kubectl get clusterrole "${name}" -ojson)

  annot_value=$(echo "${resource}" | jq -r '.metadata.annotations."configmanagement.gke.io/managed"')
  if [[ $annot_value != "enabled" ]]; then
    echo "clusterrole does not have a valid management annotation"
    return 1
  fi
  actual_value=$(echo "${resource}" | jq -c "${sel}")
  if [[ "$value" != "$actual_value" ]]; then
    echo "clusterrole${sel} is ${actual_value}, expected ${value}"
    return 1
  fi
}
