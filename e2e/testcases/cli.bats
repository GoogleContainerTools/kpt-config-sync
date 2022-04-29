#!/bin/bash

# Testcases related to nomos cli

set -euo pipefail

load "../lib/debug"
load "../lib/git"
load "../lib/resource"
load "../lib/setup"
load "../lib/wait"
load "../lib/namespace"

FILE_NAME="$(basename "${BATS_TEST_FILENAME}" '.bats')"

CMS_NS="config-management-system"
KS_NS="kube-system"

test_setup() {
  setup::git::initialize

  # Fixes a problem when running the tests locally
  if [[ -z "${KUBECONFIG+x}" ]]; then
    export KUBECONFIG="${HOME}/.kube/config"
  fi
}

@test "${FILE_NAME}: CLI bugreport.  Nomos running correctly." {
  # confirm all the pods are up
  namespace::check_exists ${KS_NS}
  # TODO: This won't work in the new test world, which has no operator
  # resource::check_count -n ${KS_NS} -r pod -c 1 -l "k8s-app=config-management-operator"

  namespace::check_exists ${CMS_NS}
  resource::check_count -n ${CMS_NS} -r pod -c 1 -l "app=git-importer"
  resource::check_count -n ${CMS_NS} -r pod -c 1 -l "app=monitor"

  nomos bugreport

  # check that zip exists
  BUG_REPORT_ZIP_NAME=$(get_bug_report_file_name)
  BUG_REPORT_DIR_NAME=${BUG_REPORT_ZIP_NAME%.zip}
  [[ -e "${BUG_REPORT_ZIP_NAME}" ]]

  /usr/bin/unzip "${BUG_REPORT_ZIP_NAME}"

  CurrentContext=$(kubectl config current-context)
  # Check that the correct files are there
  check_singleton "raw/${CurrentContext}/namespaces/config-management-system/git-importer.*/git-sync.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/namespaces/config-management-system/git-importer.*/importer.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/namespaces/config-management-system/monitor.*/monitor.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/namespaces/config-management-system/pods.txt" "${BUG_REPORT_DIR_NAME}"
  # TODO: This won't work in the new test world, which has no operator
  # check_singleton "raw/${CurrentContext}/namespaces/kube-system/config-management-operator.*/manager.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/namespaces/kube-system/pods.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "processed/${CurrentContext}/version.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "processed/${CurrentContext}/status.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/cluster/configmanagement/clusterconfigs.txt" "${BUG_REPORT_DIR_NAME}"
  # TODO: This won't work in the new test world, which has no operator
  # check_singleton "raw/${CurrentContext}/cluster/configmanagement/configmanagements.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/cluster/configmanagement/namespaceconfigs.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/cluster/configmanagement/repos.txt" "${BUG_REPORT_DIR_NAME}"
  check_singleton "raw/${CurrentContext}/cluster/configmanagement/syncs.txt" "${BUG_REPORT_DIR_NAME}"
}

function get_bug_report_file_name {
  for f in ./*
  do
    if [[ -e $(echo "${f}" | grep "bug_report_.*.zip" ) ]]; then
      basename "${f}"
    fi
  done
}

function check_singleton {
  local regex_string="${1}"
  local base_dir="${2}"
  local directory_contents
  local num_results

  directory_contents="$(cd "${base_dir}"; find . -name "*.txt")"
  num_results="$(echo "${directory_contents}" | grep -c "${regex_string}")"

  if [[ "${num_results}" -eq 1 ]]; then
    return 0
  else
    printf "ERROR: %s results found matching regex: %s\n" "${num_results}" "${regex_string}"
    echo "${directory_contents}"

    return 1
  fi
}
