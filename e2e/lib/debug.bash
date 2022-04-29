#!/bin/bash

function debug::__header() {
  local src
  src="$(basename "${BASH_SOURCE[2]}")"
  local line=${BASH_LINENO[1]}
  local func="<global>"
  if (( ${#FUNCNAME[@]} > 2 )); then
    func="(${FUNCNAME[2]})"
  fi
  echo "$src:$line $func"
}

# Sends info to tap output. This is always going to be visible during test runs,
# so it should only stream info that indicates e2e framework issues.
function debug::warn() {
  echo "# $(debug::__header)" "$@" >&3
}

# log sends info to stdout, user will see this if there is a test error, should
# be used for test info.
function debug::log() {
  echo "$(debug::__header)" "$@"
}

# error sends info to tap output and fails, this is useful for indicating
# programmer error such as invalid parameters to a function.
function debug::error() {
  echo "$(debug::__header) ERROR:" "$@"
  return 1
}
