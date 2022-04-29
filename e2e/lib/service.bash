#!/bin/bash

# Check if a Service's clusterIP exists.
#
# Arguments:
#   name: the Service name
#   namespace: the namespace the Service exists in
function service::has_cluster_ip() {
  local name="${1:-}"
  local namespace="${2:-}"
  local hasClusterIP
  hasClusterIP=$(kubectl get service "${name}" -n "${namespace}" -ojson | jq '.spec | has("clusterIP")')
  if [[  "$hasClusterIP" != "true" ]]; then
    echo "Service ${name} .spec.clusterIP field is not set"
    return 1
  fi
}