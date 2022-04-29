#!/bin/bash

set -euo pipefail

load "../lib/debug"
load "../lib/git"
load "../lib/nomos"
load "../lib/setup"
load "../lib/wait"

YAML_DIR=${BATS_TEST_DIRNAME}/../testdata/apiservice

test_setup() {
  setup::git::initialize
  setup::git::commit_minimal_repo_contents
}

test_teardown() {
  git::rm acme/cluster/apiservice.yaml
  git::commit
  kubectl delete -f "${YAML_DIR}/apiservice.yaml" --ignore-not-found
  setup::git::remove_all acme
}

@test "Create APIService and endpoint in same commit" {
  debug::log "Creating commit with APIService and Deployment"
  git::add \
    "${YAML_DIR}/namespace-custom-metrics.yaml" \
    acme/namespaces/custom-metrics/namespace-custom-metrics.yaml
  git::add \
    "${YAML_DIR}/apiservice.yaml" \
    acme/cluster/apiservice.yaml
  git::commit

  # This takes a long time since the APIService points to a deployment and we
  # have to wait for the deployment to come up.
  debug::log "Waiting for nomos to sync new APIService"
  wait::for -t 240 -- nomos::repo_synced
}

@test "importer and syncer resilient to bad APIService" {
  debug::log "Adding bad APIService"
  kubectl apply -f "${YAML_DIR}/apiservice.yaml"

  debug::log "Creating commit with Deployment"
  git::add \
    "${YAML_DIR}/namespace-resilient.yaml" \
    acme/namespaces/resilient/namespace-resilient.yaml
  git::commit

  debug::log "Waiting for nomos to stabilize"
  wait::for -t 240 -- nomos::repo_synced

  kubectl delete -f "${YAML_DIR}/apiservice.yaml" --ignore-not-found
}
