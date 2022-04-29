#!/bin/bash

set -euo pipefail

load "../lib/git"
load "../lib/setup"
load "../lib/wait"

FILE_NAME="$(basename "${BATS_TEST_FILENAME}" '.bats')"

test_setup() {
  setup::git::initialize
  setup::git::init acme
}

test_teardown() {
  setup::git::remove_all acme
}

YAML_DIR=${BATS_TEST_DIRNAME}/../testdata/multiversion

@test "${FILE_NAME}: Multiple versions of RoleBindings" {
  git::add "${YAML_DIR}/v1.yaml" acme/namespaces/eng/backend/v1.yaml
  git::commit

  wait::for -t 30 -- kubectl get rolebinding -n backend v1user

  git::rm acme/namespaces/eng/backend/v1.yaml
  git::commit

  wait::for -f -t 30 -- kubectl get rolebinding -n backend v1user
}
