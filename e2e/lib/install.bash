#!/bin/bash

# Helpers for installation

# Creates a public-private ssh key pair.
function install::create_keypair() {
  echo "+++ Creating keypair at: $TEST_DIR"
  if [[ -f "$TEST_DIR/id_rsa.nomos" ]]; then
    echo "+++ Keypair already exists"
    return
  fi
  # Skipping confirmation in keygen returns nonzero code even if it was a
  # success.
  (yes | ssh-keygen \
        -t rsa -b 4096 \
        -C "your_email@example.com" \
        -N '' -f "$TEST_DIR/id_rsa.nomos") || echo "Key created here."
}

# Returns the number of available replicas in a deployment.
#
# Use this method to figure out whether a Deployment is running or not rather
# than inspecting the underlying ReplicaSet or Pods, to avoid racing with the
# Deployment controller.  A Deployment may remain in the "unavailable" state
# long after underlying Pods are running.
#
# Args:
#   $1: namespace
#   $2: deployment name
function install::available_replicas() {
  local namespace="${1}"
  local deployment="${2}"

  kubectl get deployments --ignore-not-found \
    -n="${namespace}" "${deployment}" \
    -o 'jsonpath={.status.availableReplicas}'
}

# Returns 0 if the nomos deployment is up and running. Returns non-zero
# otherwise.
function install::nomos_running() {
  local deployment="git-importer"
  local res
  res="$(install::available_replicas config-management-system "${deployment}")"
  if [[ "${res}" == "0" ]] || [[ -z "${res}" ]]; then
    echo "${deployment} not yet running"
    return 1
  fi
  return 0
}

function install::nomos_uninstalled() {
  if [[ "$(kubectl get pods -n config-management-system | wc -l)" -ne 0 ]]; then
    echo "Nomos pods not yet uninstalled"
    return 1
  fi
  return 0
}
