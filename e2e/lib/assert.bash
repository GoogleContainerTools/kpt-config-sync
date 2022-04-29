#!/bin/bash

# Helpers for test assertions.

# assert::contains <substring>
#
# Following a run command, will fail if the output of the command
# or its error message doesn't contain substring
#
function assert::contains() {
  local str="${1:-}"
  # shellcheck disable=SC2154
  if [[ "$output" != *"${str}"* ]]; then
    echo "FAIL: [$output] does not contain [${str}]"
    false
  fi
}

# assert::equals <string>
#
# Following a run command, will fail if the output of the command
# or its error message doesn't match string
#
function assert::equals() {
  local str="${1:-}"
  # shellcheck disable=SC2154
  if [[ "$output" != "${str}" ]]; then
    echo "FAIL: [$output] does not equal [${str}]"
    false
  fi
}

# assert::not_equals <string>
#
# Following a run command, will fail if the output of the command
# or its error message matches string
#
function assert::not_equals() {
  local str="${1:-}"
  # shellcheck disable=SC2154
  if [[ "$output" == "${str}" ]]; then
    echo "FAIL: [$output] does equal [${str}]"
    false
  fi
}
