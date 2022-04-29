#!/bin/bash

set -euo pipefail

load "../lib/env"
load "../lib/git"
load "../lib/configmap"
load "../lib/setup"
load "../lib/wait"
load "../lib/nomos"

FILE_NAME="$(basename "${BATS_TEST_FILENAME}" '.bats')"

test_setup() {
  if ! env::csmr; then
    kubectl apply -f "${MANIFEST_DIR}/importer_acme.yaml"
    kubectl apply -f "${MANIFEST_DIR}/source-format_hierarchy.yaml"
    nomos::restart_pods
  fi

  setup::git::initialize
  setup::git::init acme
}

test_teardown() {
  nomos::reset_mono_repo_configmaps
  setup::git::remove_all acme
}

set_importer_no_policy_dir() {
  if env::csmr; then
    # No teardown required since the Go framework runs one test per setup
    kubectl patch -n config-management-system rootsync root-sync \
      --type merge \
      --patch '{"spec":{"git":{"dir":""}}}'
  else
    kubectl apply -f "${MANIFEST_DIR}/importer_no-policy-dir.yaml"
    nomos::restart_pods
  fi
}

@test "${FILE_NAME}: Absence of cluster, namespaces, systems directories at POLICY_DIR yields a missing repo error" {
  set_importer_no_policy_dir

  # Verify that the application of the operator config yields the correct error code
  nomos::import_error_code "1017"
}

# This test case gradually adjusts root and acme directories' contents to move from:
# POLICY_DIR == "acme" to: POLICY_DIR undefined in the template.  Thus the gradual steps.
@test "${FILE_NAME}: Confirm that nomos syncs correctly with POLICY_DIR unset" {
  setup::git::add_contents_to_root acme

  set_importer_no_policy_dir

  setup::git::remove_all_from_root

  wait::for -t 60 -- nomos::repo_synced
  nomos::import_error_code ""
}
