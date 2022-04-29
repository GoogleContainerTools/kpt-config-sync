#!/bin/bash

set -euo pipefail

DIR=$(dirname "${BASH_SOURCE[0]}")
# shellcheck source=e2e/lib/debug.bash
source "$DIR/debug.bash"

# Helpers for changing the state of the e2e source-of-truth git
# repository. Prepare the next state to commit using git::add, git::update, and
# git::rm and then commit these changes to the sot repo with git::commit.
#
# Example Usage:
# git::add ${TEST_YAMLS}/new_namespace.yaml acme/rnd/newest-prj/namespace.yaml
# git::update ${TEST_YAMLS}/updated_quota.yaml acme/eng/quota.yaml
# git::rm acme/rnd/new-prj/acme-admin-role.yaml

TEST_REPO=${BATS_TMPDIR}/repo

# Create a new file or directory in the sot directory.
#
# Usage:
# git::add SOURCE DEST
#
# SOURCE is the path of the source file/directory
# DEST is the path within the sot directory to copy to
#
# Directories that don't exist in the DEST_LOC path will be created.
# If both params are not provided and non-empty, or if the destination
# already exists, the function will fail the containing bats test.
#
function git::add() {
  local src=$1
  local dst=$2

  cd "${TEST_REPO}"

  [ -n "$src" ] || (echo "source not provided to git::add" && false)
  [ -n "$dst" ] || (echo "destination not provided to git::add" && false)
  [ ! -e "$dst" ] || (echo "$dst already exists but should not" && false)

  mkdir -p "$(dirname "$dst")"
  cp -r "$src" "./$dst"
  echo "git: add $dst"
  git add "$dst"

  cd -
}

# Update a file in the sot directory.
#
# Usage:
# git::update SOURCE_FILE DEST_LOC
#
# SOURCE_FILE is the location of the source yaml to copy
# DEST_LOC is the path within the sot directory to copy to
#
# If both params are not provided and non-empty, or if the destination
# does not already exist, the function will fail the containing bats test.
#
function git::update() {
  local src=$1
  local dst=$2

  cd "${TEST_REPO}"

  [ -n "$src" ] || (echo "source not provided to git::update" && false)
  [ -n "$dst" ] || (echo "destination not provided to git::update" && false)
  [ -f "$dst" ] || (echo "$dst does not already exist but should" && false)

  cp "$src" "./$dst"
  echo "git: add $dst (updated from $src)"
  git add "$dst"

  cd -
}

# Delete a file or directory in the sot directory.
#
# Usage:
# git::rm FILEPATH
#
# FILEPATH is the path within the sot directory to delete
#
# If the param is not provided and non-empty, or if the filepath
# does not already exist, the function will fail the containing bats test.
#
function git::rm() {
  local path=$1

  cd "${TEST_REPO}"

  [ -n "$path" ] || (echo "filename not provided to git::rm" && false)
  [ -e "$path" ] || (echo "$path does not already exist but should" && false)

  echo "git: rm -r $path"
  git rm -r "$path"

  cd -
}

# Commit and push the add, update, and rm operations that are queued
#
# Usage:
# git::commit
#
# Flags:
# -m [message] - set a custom commit message instead of the default one
function git::commit() {
  local message="commit for test"
  local args=()
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -m)
        message=${1:-}
        shift
        ;;
      *)
        args+=("$arg")
        ;;
    esac
  done
  cd "${TEST_REPO}"

  echo "git: commit / push"
  git commit -m "${message}"
  echo "git: pushing $(git log -n1 --format=format:%H)"
  git push -u origin main:main -f

  cd -
}

# Get the current Git hash
function git::hash() {
  git -C "${TEST_REPO}" log -n1 --format=%H
}

# Check that the current commit hash equals the specified value.
#
# Usage:
# git::check_hash "myhash"
#
function git::check_hash() {
  local expected=$1
  local cur_hash
  cur_hash="$(git::hash)"
  if [[ "${cur_hash}" != "${expected}" ]]; then
    debug::error "Git hash mismatch, expected ${expected} got ${cur_hash}"
  fi
}

