#!/bin/bash

set -euo pipefail

DIR=$(dirname "${BASH_SOURCE[0]}")
NOMOS_DIR=$(readlink -f "${DIR}/../..")
FWD_SSH_PORT=${FWD_SSH_PORT:-2222}
SAFETY_CHECK_NAMESPACE="safety-config-management-system-root-sync"

# shellcheck source=e2e/lib/debug.bash
source "$DIR/debug.bash"
# shellcheck source=e2e/lib/git.bash
source "$DIR/git.bash"
# shellcheck source=e2e/lib/ignore.bash
source "$DIR/ignore.bash"
# shellcheck source=e2e/lib/namespace.bash
source "$DIR/namespace.bash"
# shellcheck source=e2e/lib/nomos.bash
source "$DIR/nomos.bash"
# shellcheck source=e2e/lib/resource.bash
source "$DIR/resource.bash"
# shellcheck source=e2e/lib/wait.bash
source "$DIR/wait.bash"

# Git-specific repository initialization.
setup::git::initialize() {
  # Reset git repo to initial state.
  echo "Setting up local git repo in ${TEST_REPO}"

  rm -rf "${TEST_REPO:?}"
  mkdir -p "${TEST_REPO}"
  cd "${TEST_REPO}"

  git init
  git checkout -b main
  git remote add origin "ssh://git@localhost:${FWD_SSH_PORT}/git-server/repos/config-management-system/root-sync"
  git fetch
  git config user.name "Testing Nome"
  git config user.email testing_nome@example.com
}


setup::git::__init_dir() {
  local DIR_NAME=${1}

  cd "${TEST_REPO}"

  mkdir "${DIR_NAME}"
  touch "${DIR_NAME}/README.md"
  git add "${DIR_NAME}/README.md"
  git commit -a -m "initial commit"

  cp -r "${NOMOS_DIR}/examples/${DIR_NAME}" ./
}

setup::git::__commit_dir_and_wait() {
  cd "${TEST_REPO}"
  git add -A
  git status
  git commit -m "setUp commit"
  git push -u origin main:main -f

  wait::for -t 60 -- nomos::repo_synced
}

# Adds an example repo to git, pushes it, and blocks until the proper namespaces are created.
setup::git::init() {
  local DIR_NAME=${1}

  setup::git::__init_dir "${DIR_NAME}"
  setup::git::__commit_dir_and_wait
}

# Same as init, but allows for specified files/directories to be excluded from an example repo.
#
# Variadic Args:
#   The paths of any files/directories from the example repo to exclude
# Example:
# - To exclude directory foo/bar/ and file foo/bazz.yaml:
#   setup::git::init_without() foo -- foo/bar/ foo/bazz.yaml
setup::git::init_without() {
  if [[ $# -eq 0 ]]; then
    echo "Error: No paths supplied to 'init_without'. Use 'init' if there are no paths to exclude"
    exit 1
  fi

  if [[ $# -lt 3 ]]; then
    echo "Error: must supply both example directory and files to exclude, delineated by '--'.  Use 'init' if there are no paths to exclude"
    exit 1
  fi

  if [[ ! " $* " =~ " -- " ]]; then
    echo "Error: missing '--'.  Must delineate repo and files using like: exampleDir -- exampleDir/aFile exampleDir/anotherFile"
    exit 1
  fi

  if [[ $2 != "--" ]]; then
    echo "Error: misplaced '--'.  Proper format: exampleDir -- exampleDir/aFile exampleDir/anotherFile"
  fi

  local DIR_NAME=$1
  shift 2

  setup::git::__init_dir "${DIR_NAME}"
  for path in "$@"; do
    if [[ -d ${path} ]]; then
      rm -r ${path}
    elif [[ -f ${path} ]]; then
      rm $path
    elif ! [[ -e ${path} ]]; then
      echo "Error: Specified path $path does not exist in the specified repo"
      exit 1
    fi
  done
  setup::git::__commit_dir_and_wait
}

# Removes *almost* all managed resources of the specified repo from git, pushes it, and blocks until complete.
setup::git::remove_all() {
  local DIR_NAME=${1}

  rm -rf "${TEST_REPO:?}/${DIR_NAME}/cluster"
  rm -rf "${TEST_REPO:?}/${DIR_NAME}/namespaces"

  mkdir -p "${TEST_REPO}/${DIR_NAME}/cluster"
  cp "${NOMOS_DIR}/examples/${DIR_NAME}/cluster/admin-clusterrole.yaml" "${TEST_REPO}/${DIR_NAME}/cluster/admin-clusterrole.yaml"
  namespace::declare "${SAFETY_CHECK_NAMESPACE}" -l "testdata=true"

  cd "${TEST_REPO}"
  git add -A
  git status
  git commit -m "wiping almost all of ${DIR_NAME}"
  git push -u origin main:main -f

  wait::for -t 60 -- nomos::repo_synced
  wait::for -t 60 -- kubectl get ns "${SAFETY_CHECK_NAMESPACE}"
  wait::for -t 60 -- kubectl get clusterrole acme-admin

  setup::git::remove_all_dangerously "${DIR_NAME}"
}

# Removes all managed resources of the specified repo from git, pushes it, and blocks until complete.
setup::git::remove_all_dangerously() {
  local DIR_NAME=${1}

  rm -rf "${TEST_REPO:?}/${DIR_NAME}/cluster"
  rm -rf "${TEST_REPO:?}/${DIR_NAME}/namespaces"

  cd "${TEST_REPO}"
  git add -A
  git status
  git commit -m "wiping contents of ${DIR_NAME}"
  git push -u origin main:main -f

  wait::for -t 60 -- nomos::repo_synced
  if ! env::csmr; then
    # Unclear if these are needed given nomos::repo_synced
    wait::for -f -t 60 -- kubectl get namespaceconfig "${SAFETY_CHECK_NAMESPACE}"
    wait::for -f -t 60 -- kubectl get clusterrole acme-admin
  fi
}

# Adds the contents of the specified example directory to the git repository's root
#
# This is part of the tests that evaluate behavior with undefined POLICY_DIR
#
setup::git::add_contents_to_root() {
  local DIR_NAME=${1}

  cp -r "${NOMOS_DIR}/examples/${DIR_NAME}/"* "${TEST_REPO}/"

  cd "${TEST_REPO}"
  git add -A
  git commit -m "add files to root"
  git push -u origin main:main -f

  wait::for -t 60 -- nomos::repo_synced
}

setup::git::commit_minimal_repo_contents() {
  echo "+++ Setting up minimal repo contents."
  local skip_commit=false
  local skip_wait=false
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      --skip_commit)
        skip_commit=true
      ;;
      --skip_wait)
        skip_wait=true
      ;;
      *)
        echo "Unexpected arg $arg" >&3
        return 1
      ;;
    esac
  done
  cd "${TEST_REPO}"
  mkdir -p acme/system
  cp -r "${NOMOS_DIR}/examples/acme/system" acme
  git add -A
  if $skip_commit; then
    echo "+++ Skipping setup commit"
    return
  fi
  git::commit -a -m "Commit minimal repo contents."
  if $skip_wait; then
    echo "+++ Skipping setup wait"
    return
  fi
  wait::for -t 60 -- nomos::repo_synced
}

# This removes the namespaces/ and cluster/ subdirectories from the repo root.
#
# This is part of the tests that evaluate behavior with undefined POLICY_DIR
#
setup::git::remove_all_from_root() {
  rm -rf "${TEST_REPO:?}/cluster"
  rm -rf "${TEST_REPO:?}/namespaces"

  # Add a cluster-scoped resource and a namespace to avoid failing the safety check.
  mkdir -p "${TEST_REPO}/cluster"
  cp "${NOMOS_DIR}/examples/acme/cluster/admin-clusterrole.yaml" "${TEST_REPO}/cluster/admin-clusterrole.yaml"

  mkdir -p "${TEST_REPO}/namespaces/new-prj"
  cp "${NOMOS_DIR}/examples/acme/namespaces/rnd/new-prj/namespace.yaml" "${TEST_REPO}/namespaces/new-prj/namespace.yaml"

  cd "${TEST_REPO}"
  git add -A
  git status
  git commit -m "wiping almost all of root"
  git push -u origin main:main -f

  wait::for -t 60 -- nomos::repo_synced
  wait::for -t 60 -- kubectl get ns new-prj
  wait::for -t 60 -- kubectl get clusterrole acme-admin

  rm -rf "cluster"
  rm -rf "namespaces"

  git add -A
  git status
  git commit -m "wiping contents of root"
  git push -u origin main:main -f

  wait::for -t 60 -- nomos::repo_synced
}

setup::__skip_test() {
  E2E_TEST_FILTER="${E2E_TEST_FILTER:-}"
  if [[ "${E2E_TEST_FILTER}" != "" ]]; then
    local cur_test=""
    for func in "${FUNCNAME[@]}"; do
      if echo "$func" | grep "^test_" &> /dev/null; then
        cur_test="${func}"
      fi
    done
    if ! echo "${cur_test}" | grep "${E2E_TEST_FILTER}" &> /dev/null; then
      return
    fi
  fi

  E2E_RUN_TEST_NUM="${E2E_RUN_TEST_NUM:-}"
  if [[ "${E2E_RUN_TEST_NUM}" != "" ]]; then
    if [[ "${E2E_RUN_TEST_NUM}" != "${BATS_TEST_NUMBER}" ]]; then
      return
    fi
  fi
  return 1
}

# Clean up test resources.
setup::cleanup() {
  kubectl delete --ignore-not-found apiservices -l "testdata=true"
  kubectl delete --ignore-not-found ns -l "testdata=true"
  resource::delete -r ns -a configmanagement.gke.io/managed=enabled
}

# Record timing info, and implement the test filter. This is cheap, and all tests should do it.
setup() {
  START_TIME=$(date +%s%3N)
  if setup::__skip_test; then
    skip
  fi

  setup::cleanup
  # declare -f -F exits true if test_setup is defined as a function
  if declare -f -F test_setup > /dev/null; then
    test_setup
  fi
}

# Record and print timing info. This is cheap, and all tests should do it.
teardown() {
  # only teardown if ran the test (bats will run teardown even if we skip in setup).
  if ! setup::__skip_test; then
    # declare -f -F exits true if test_teardown is defined as a function
    if declare -f -F test_teardown > /dev/null; then
      test_teardown
    fi
  fi

  setup::cleanup
  local end
  end=$(date +%s%3N)
  ((runtime="$end - $START_TIME"))
  if [ -n "${TIMING:+1}" ]; then
    echo "# TAP2JUNIT: Duration: ${runtime}ms" >&3
  fi
}
