#!/bin/bash

set -euo pipefail

DIR=$(dirname "${BASH_SOURCE[0]}")
# shellcheck source=e2e/lib/assert.bash
source "$DIR/assert.bash"
# shellcheck source=e2e/lib/debug.bash
source "$DIR/debug.bash"
# shellcheck source=e2e/lib/resource.bash
source "$DIR/resource.bash"
# shellcheck source=e2e/lib/wait.bash
source "$DIR/wait.bash"

# Helpers for creating namespaces/abstract namespaces in git.

# Directly creates a namespace on the cluster with optional label for nomos management.
#
# Arguments
#   name: The namespace name
# Parameters
#   -l [label] a label for the namespace (repeated)
#   -a [annotation] an annotation for the namespace (repeated)
function namespace::create() {
  local tmp
  tmp="$(mktemp -up "${BATS_TMPDIR}" ns-XXXXXX).yaml"
  namespace::__genyaml -l 'testdata=true' "$@" "${tmp}"
  echo "Creating Cluster Namespace:"
  cat "${tmp}"
  echo
  kubectl apply -f "${tmp}"
}

# Creates a namespace directory and yaml in the git repo.  If abstract namespaces are
# required based on the path they will be implicitly created as well.
#
# Arguments
#   path: The path to the namespace directory under acme with the last portion being the namespace name.
# Parameters
#   -l [label] a label for the namespace (repeated)
#   -a [annotation] an annotation for the namespace (repeated)
function namespace::declare() {
  local args=()
  local genyaml_args=()
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -a)
        genyaml_args+=("-a" "${1:-}")
        shift
      ;;
      -l)
        genyaml_args+=("-l" "${1:-}")
        shift
      ;;
      *)
        args+=("${arg}")
      ;;
    esac
  done

  local path="${args[0]:-}"
  [ -n "$path" ] || debug::error "Must specify namespace name"

  local name
  name="$(basename "${path}")"
  local dst="acme/namespaces/${path}/namespace.yaml"
  local abs_dst="${TEST_REPO}/${dst}"
  genyaml_args+=("${name}" "${abs_dst}")
  namespace::__genyaml "${genyaml_args[@]}"
  echo "Declaring namespace:"
  cat "${abs_dst}"
  git -C "${TEST_REPO}" add "${dst}"
}

# Checks that a namespace exists on the cluster.
#
# Arguments
#   name: The name of the namespace
# Params
#   -l [label]: the value of a label to check, formatted as "key=value"
#   -a [annotation]: an annotation to check for, formatted as "key=value"
#   -t [timeout_sec]: set a timeout to other than default, example: "-t 42"
function namespace::check_exists() {
  local args=()
  local check_args=()
  local timeout_sec="200"
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -a)
        check_args+=(-a "${1:-}")
        shift
      ;;
      -l)
        check_args+=(-l "${1:-}")
        shift
      ;;
      -t)
        timeout_sec="${1}"
        shift
      ;;
      *)
        args+=("${arg}")
      ;;
    esac
  done

  local name=${args[0]:-}
  [ -n "$name" ] || debug::error "Must specify namespace name"

  wait::for -t "${timeout_sec}" -- kubectl get ns "${name}"

  if [[ "${#check_args[@]}" != 0 ]]; then
    resource::check ns "${name}" "${check_args[@]}"
  fi
}

# Checks that a namespace does not exist
#
# Arguments
#   name: The name of the namespace
# Params
#   -t [timeout_sec]: set a timeout to other than default, example: "-t 42"
function namespace::check_not_found() {
  local args=()
  local timeout_sec="60"
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -t)
        timeout_sec="${1}"
        shift
      ;;
      *)
        args+=("${arg}")
      ;;
    esac
  done
  local ns="${args[0]}"
  wait::for -f -t "${timeout_sec}" -- kubectl get ns "${ns}"
  run kubectl get ns "${ns}"
  assert::contains "NotFound"
}

# Internal function
# Generates namespace yaml
# Parameters
#   -l [label] a label for the namespace (repeated)
#   -a [annotation] an annotation for the namespace (repeated)
# Arguments
#   [name] the namespace name
#   [filename] the file to create
function namespace::__genyaml() {
  debug::log "namespace::__genyaml" "$@"
  local args=()
  local annotations=()
  local labels=()
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -a)
        annotations+=("${1:-}")
        shift
      ;;
      -l)
        labels+=("${1:-}")
        shift
      ;;
      *)
        args+=("${arg}")
      ;;
    esac
  done
  local name="${args[0]:-}"
  local filename="${args[1]:-}"
  [ -n "$name" ] || debug::error "Must specify namespace name"
  [ -n "$filename" ] || debug::error "Must specify yaml filename"

  mkdir -p "$(dirname "$filename")"
  cat > "$filename" << EOM
apiVersion: v1
kind: Namespace
metadata:
  name: ${name}
EOM
  if (( ${#labels[@]} != 0 )); then
    echo "  labels:" >> "$filename"
    local label
    for label in "${labels[@]}"; do
      namespace::__yaml::kv "    " "$label" >> "$filename"
    done
  fi
  if (( ${#annotations[@]} != 0 )); then
    echo "  annotations:" >> "$filename"
    local annotation
    for annotation in "${annotations[@]}"; do
      namespace::__yaml::kv "    " "$annotation" >> "$filename"
    done
  fi
}

# Internal. Creates a yaml key-value pair for labels / annotations
# Params
#   [indent] spaces for indent
#   [value] keyvalue formatted as "key=value"
function namespace::__yaml::kv() {
  local indent=${1:-}
  local keyval="${2:-}"
  local key
  local value
  key=$(sed -e 's/=.*//' <<< "${keyval}")
  value=$(sed -e 's/[^=]*=//' <<< "${keyval}")
  case "$value" in
    [0-9]*|true|false)
      value="\"$value\""
      ;;
  esac
  echo "${indent}${key}: ${value}"
}

