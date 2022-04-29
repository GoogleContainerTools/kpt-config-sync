#!/bin/bash

set -euo pipefail

load "../lib/debug"
load "../lib/git"
load "../lib/nomos"
load "../lib/setup"
load "../lib/wait"

YAML_DIR=${BATS_TEST_DIRNAME}/../testdata/gatekeeper

test_setup() {
  setup::git::initialize
  setup::git::commit_minimal_repo_contents
}

test_teardown() {
  kubectl delete -f "${YAML_DIR}/constraint-template-crd.yaml" --ignore-not-found
  kubectl delete -f "${YAML_DIR}/constraint-crd.yaml" --ignore-not-found
  setup::git::remove_all acme
}

@test "constraint template and constraint in same commit" {
  debug::log "Adding gatekeeper CT CRD"
  # Don't do strict validation since the CRD includes fields introduced in 1.13.
  kubectl apply -f "${YAML_DIR}/constraint-template-crd.yaml" --validate=false

  debug::log "Adding CT/C in one commit"
  git::add \
    "${YAML_DIR}/constraint-template.yaml" \
    acme/cluster/constraint-template.yaml
  git::add \
    "${YAML_DIR}/constraint.yaml" \
    acme/cluster/constraint.yaml
  git::commit

  debug::log "Waiting for CT on API server"
  wait::for -t 120 -- kubectl get constrainttemplate k8sallowedrepos

  debug::log "Applying CRD for constraint (simulates gatekeeper)"
  kubectl apply -f "${YAML_DIR}/constraint-crd.yaml" --validate=false

  debug::log "Waiting for nomos to recover"
  wait::for -t 120 -- nomos::repo_synced
}
