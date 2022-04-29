#!/bin/bash

set -euo pipefail

# Returns success if the test is running in multi repo mode.
function env::csmr() {
  if ${CSMR:-false}; then
    return 0
  fi
  return 1
}
