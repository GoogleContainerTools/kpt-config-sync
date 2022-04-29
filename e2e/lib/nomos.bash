#!/bin/bash

set -euo pipefail

DIR=$(dirname "${BASH_SOURCE[0]}")
# shellcheck source=e2e/lib/debug.bash
source "$DIR/debug.bash"
# shellcheck source=e2e/lib/env.bash
source "$DIR/env.bash"
# shellcheck source=e2e/lib/git.bash
source "$DIR/git.bash"
# shellcheck source=e2e/lib/wait.bash
source "$DIR/wait.bash"
# shellcheck source=e2e/lib/resource.bash
source "$DIR/resource.bash"

# Helper funciton for NS and Cluster synced check.
function nomos::_synced() {
  if (( $# != 3 )); then
    debug::error "Invalid number of args"
    return 1
  fi

  local output="${1:-}"
  local token="${2:-}"
  local error="${3:-}"
  if [[ "$output" == "" ]]; then
    return 1
  fi

  # extract tokens, error count from json output
  local spec_token
  local status_token
  local error_count
  spec_token="$(jq -r '.spec.token' <<< "$output")"
  status_token="$(jq -r '.status.token' <<< "$output")"
  error_count="$(jq '.status.syncErrors | length' <<< "$output")"

  # compare tokens, check error count
  if [[ "$token" != "$spec_token" ]]; then
    return 1
  fi
  if [[ "$token" != "$status_token" ]]; then
    return 1
  fi
  if $error; then
    if ((0 == error_count)); then
      return 1
    fi
  else
    if ((0 < error_count)); then
      return 1
    fi
  fi
}

# Returns success if the repo is fully synced.
function nomos::repo_synced() {
  if env::csmr; then
    nomos::__csmr_repo_synced
  else
    nomos::__legacy_repo_synced
  fi
}

# Returns success if the repo is fully synced.
# This is defined as the source and sync status being equaal to the current git commit hash.
function nomos::__csmr_repo_synced() {
  local rs
  rs="$(kubectl get -n config-management-system rootsync root-sync -ojson)"
  if [[ $(jq -r ".status.source.commit" <<< "$rs") != $(git::hash) ]]; then
    return 1
  fi
  if [[ $(jq -r ".status.sync.commit" <<< "$rs") != $(git::hash) ]]; then
    return 1
  fi
}

# Returns success if the repo is fully synced.
# This is defined as
#   token == status.source.token && len(status.source.errors) == 0 \
#   && token == status.import.token && len(status.import.errors) == 0 \
#   && token == status.sync.token && len(status.sync.errors) == 0 \
#   && status.sync.inProgress == null
function nomos::__legacy_repo_synced() {
  if (( $# != 0 )); then
    echo "Invalid number of args for repo_synced"
    return 1
  fi

  local token
  token="$(git::hash)"

  local output
  # This fixes a race condition for when the repo object is deleted from git SOT
  # as part of the testing scenario
  wait::for -t 60 -p 1 -- resource::check repo repo
  output="$(kubectl get repo repo -ojson)"

  local status_token
  local error_count
  status_token="$(jq -r '.status.source.token' <<< "$output")"
  error_count="$(jq '.status.source.errors | length' <<< "$output")"
  if [[ "$token" != "$status_token" ]]; then
    return 1
  fi
  if (( 0 < error_count )); then
    return 1
  fi

  status_token="$(jq -r '.status.import.token' <<< "$output")"
  error_count="$(jq '.status.import.errors | length' <<< "$output")"
  if [[ "$token" != "$status_token" ]]; then
    return 1
  fi
  if (( 0 < error_count )); then
    return 1
  fi

  status_token="$(jq -r '.status.sync.latestToken' <<< "$output")"
  if [[ "$token" != "$status_token" ]]; then
    return 1
  fi

  in_progress="$(jq '.status.sync.inProgress' <<< "$output")"
  if [[ "$in_progress" != "null" ]]; then
    return 1
  fi

  #### WORKAROUND ####
  # Right now we do not yet report status correctly on deleting namespaces.
  # This check will ensure that we do not report synced if there are still
  # managed namespaces which do not have an associated NamespaceConfig object.
  ####################
  local ns_count
  local nc_count
  ns_count="$(
    kubectl get ns -ojson \
    | jq '[.items[] | select(.metadata.annotations["configmanagement.gke.io/managed"] == "enabled") ] | length'
  )"
  nc_count="$(kubectl get nc -ojson | jq '.items | length')"
  nc_count_disabled="$(kubectl get nc -ojson | jq '[.items[] | select(.metadata.annotations["configmanagement.gke.io/managed"] == "disabled") ] | length')"
  if (( ns_count != nc_count - nc_count_disabled )); then
    return 1
  fi
  #### END WORKAROUND ####
}

# Restarts the git-importer and monitor pods.
# Without the operator, it's necessary to restart these pods for them to pick up
# changes to their ConfigMaps.
function nomos::restart_pods() {
  if env::csmr; then
    kubectl delete pod -n config-management-system -l app=reconciler-manager
  else
    kubectl delete pod -n config-management-system -l app=git-importer
    kubectl delete pod -n config-management-system -l app=monitor
  fi
}

function nomos::source_error_code() {
  local code="${1:-}"
  if ! env::csmr && [[ "$code" != "" ]]; then
    code="KNV${code}"
  fi
  wait::for -t 90 -o "${code}" -- nomos::__source_error_code
}

function nomos::__source_error_code() {
  if env::csmr; then
    kubectl get -n config-management-system rootsync root-sync \
        --output='jsonpath={.status.source.errors[0].code}'
  else
    kubectl get repo repo -o=jsonpath='{.status.source.errors[0].code}'
  fi
}

function nomos::import_error_code() {
  local code="${1:-}"
  if ! env::csmr && [[ "$code" != "" ]]; then
    code="KNV${code}"
  fi
  wait::for -t 90 -o "${code}" -- nomos::__import_error_code
}

function nomos::__import_error_code() {
  if env::csmr; then
    kubectl get -n config-management-system rootsync root-sync \
        --output='jsonpath={.status.source.errors[0].code}'
  else
    kubectl get repo repo -o=jsonpath='{.status.import.errors[0].code}'
  fi
}

# Resets the mono-repo configmaps to the initial state.
# It also restarts the nomos pods to pick up the new config.
function nomos::reset_mono_repo_configmaps() {
  if ! env::csmr; then
    # Replace GIT_REPO_URL with the local test git-server URL.
    # Bats tests will be skipped with other git providers.
    sed -e "s|GIT_REPO_URL|git@test-git-server.config-management-system-test:/git-server/repos/config-management-system/root-sync|g" \
      "${MANIFEST_DIR}/mono-repo-configmaps.yaml" \
      | tee mono-repo-configmaps.copy.yaml \
      | kubectl apply -f -
    rm mono-repo-configmaps.copy.yaml
    nomos::restart_pods
  fi
}