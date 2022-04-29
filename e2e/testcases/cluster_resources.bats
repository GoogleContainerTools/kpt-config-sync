#!/bin/bash

set -u

load "../lib/clusterrole"
load "../lib/debug"
load "../lib/git"
load "../lib/resource"
load "../lib/setup"
load "../lib/wait"

FILE_NAME="$(basename "${BATS_TEST_FILENAME}" '.bats')"

test_setup() {
  setup::git::initialize
  setup::git::commit_minimal_repo_contents --skip_wait
}

test_teardown() {
  setup::git::remove_all acme
}

YAML_DIR=${BATS_TEST_DIRNAME}/../testdata

function sync_token_eq() {
  local expect="${1:-}"
  local stoken
  stoken="$(kubectl get clusterconfig -ojsonpath='{.items[0].status.token}')"
  if [[ "$stoken" != "$expect" ]]; then
    echo "sync token is $stoken waiting for $expect"
    return 1
  fi
}

function resource_field_eq() {
  local res="${1:-}"
  local resname="${2:-}"
  local jsonpath="${3:-}"
  local expect="${4:-}"

  # wait for resource creation
  wait::for -t 20 -- kubectl get ${res} ${resname}

  # check selection for correct value
  local selection
  selection=$(kubectl get ${res} ${resname} -ojson | jq -c "${jsonpath}")
  [[ "$selection" == "$expect" ]] || debug::error "$selection != $expect"
}

function check_cluster_scoped_resource() {
  local res="${1:-}"
  local jsonpath="${2:-}"
  local create="${3:-}"
  local modify="${4:-}"
  local resext="${5:-}"

  local resname=e2e-test-${res}
  local respath=acme/cluster/e2e-${res}.yaml

  [ -n "$res" ]
  [ -n "$jsonpath" ]
  [ -n "$create" ]
  [ -n "$modify" ]

  git::add ${YAML_DIR}/${res}${resext}-create.yaml ${respath}
  git::commit

  # check selection for correct value
  wait::for -t 30 -- resource_field_eq ${res} ${resname} ${jsonpath} ${create}

  git::update ${YAML_DIR}/${res}${resext}-modify.yaml ${respath}
  git::commit

  # wait for update
  wait::for -t 60 -- nomos::repo_synced
  selection=$(kubectl get ${res} ${resname} -ojson | jq -c "${jsonpath}")
  [[ "$selection" == "$modify" ]] || debug::error "$selection != $modify"

  # verify that import token has been updated from the commit above
  local itoken="$(kubectl get clusterconfig -ojsonpath='{.items[0].spec.token}')"
  git::check_hash "$itoken"

  # verify that sync token has been updated as well
  wait::for -- sync_token_eq "$itoken"

  # delete item
  git::rm ${respath}
  git::commit

  # check removed
  wait::for -f -- kubectl get ${res} ${resname}
}

@test "${FILE_NAME}: Local kubectl apply to clusterroles gets reverted" {
  local clusterrole_name
  clusterrole_name="e2e-test-clusterrole"

  debug::log "Ensure the clusterrole does not already exist"
  # If the test exits unexpectedly, this ClusterRole doesn't get cleaned up and
  # the test is never able to pass again, so we delete it manually here.
  kubectl delete clusterroles e2e-test-clusterrole --ignore-not-found
  wait::for -f -t 10 -- kubectl get clusterrole ${clusterrole_name}

  debug::log "Add initial clusterrole"
  git::add ${YAML_DIR}/clusterrole-modify.yaml acme/cluster/clusterrole.yaml
  git::commit -m "Add initial clusterrole"
  wait::for -t 60 -- nomos::repo_synced

  debug::log "Check initial state of the object"
  clusterrole::validate_management_selection ${clusterrole_name} '.rules[0].verbs' '["get","list","create"]'

  debug::log "Modify the clusterrole on the apiserver to remove 'create' verb from the role"
  kubectl apply --filename="${YAML_DIR}/clusterrole-create.yaml"
  debug::log "Nomos should revert the permission to what it is in the source of truth"
  wait::for -t 20 -- \
    clusterrole::validate_management_selection ${clusterrole_name} '.rules[0].verbs' '["get","list","create"]'
}

@test "${FILE_NAME}: Lifecycle for clusterroles" {
  check_cluster_scoped_resource \
    clusterrole \
    ".rules[].verbs" \
    '["get","list"]' \
    '["get","list","create"]'
}
