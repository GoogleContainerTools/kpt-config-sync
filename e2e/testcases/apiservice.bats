#!/bin/bash

set -euo pipefail

load "../lib/debug"
load "../lib/git"
load "../lib/nomos"
load "../lib/setup"
load "../lib/wait"

YAML_DIR=${BATS_TEST_DIRNAME}/../testdata/apiservice

test_setup() {
  kubectl apply -f "${MANIFEST_DIR}/source-format_unstructured.yaml"
  nomos::restart_pods
  setup::git::initialize
}

test_teardown() {
  # Remove test APIService to prevent Discovery failure blocking test repo cleanup
  git::rm acme/cluster/apiservice.yaml
  git::commit
  wait::for -t 120 -- nomos::repo_synced

  # Delete resources applied by Kubectl
  kubectl delete -f "${YAML_DIR}/apiservice.yaml" --ignore-not-found
  kubectl delete -f "${YAML_DIR}/rbac.yaml" --ignore-not-found
  kubectl delete -f "${YAML_DIR}/namespace.yaml" --ignore-not-found
  kubectl delete -f "${YAML_DIR}/namespace-custom-metrics.yaml" --ignore-not-found

  setup::git::remove_all acme
  # Restore the resource format to hierarchical mode in shared test env
  kubectl apply -f "${MANIFEST_DIR}/source-format_hierarchy.yaml"
  nomos::restart_pods
}

@test "Create APIService and endpoint in same commit" {
  debug::log "Creating commit with APIService and Deployment"
  git::add \
      "${YAML_DIR}/namespace.yaml" \
      acme/namespaces/custom-metrics/namespace.yaml
  git::add \
    "${YAML_DIR}/rbac.yaml" \
    acme/cluster/rbac.yaml
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

@test "importer and syncer resilient to flaky APIService" {
  # Create an APIService without backend so that Config Sync DiscoveryClient will
  # get service unavailable errors when trying to parse resource
  debug::log "Adding bad APIService"
  kubectl apply -f "${YAML_DIR}/apiservice.yaml"

  debug::log "Creating a commit with some test resources"
  git::add \
    "${YAML_DIR}/namespace-resilient.yaml" \
    acme/namespaces/resilient/namespace-resilient.yaml
  git::commit

  debug::log "Waiting for APIServerError to be present in sync status"
  nomos::source_error_code "2002"

  # Restore the backend for the bad APIservice and see if Config Sync can sync
  # the previous commit
  debug::log "Adding APIService backend"
  kubectl apply -f "${YAML_DIR}/namespace.yaml"
  kubectl apply -f "${YAML_DIR}/rbac.yaml"
  kubectl apply -f "${YAML_DIR}/namespace-custom-metrics.yaml"
  debug::log "Waiting for nomos to stabilize"
  wait::for -t 240 -- nomos::repo_synced
}
