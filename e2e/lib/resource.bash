#!/bin/bash

set -euo pipefail

DIR=$(dirname "${BASH_SOURCE[0]}")
# shellcheck source=e2e/lib/debug.bash
source "$DIR/debug.bash"

declare status


# Checks the existence and labels of a given resource.
# Flags:
#  -n [namespace] get the count for a namespaced resource
#  -l [label] (repeated) specify a label that must exist on the resource
#  -a [annotation] (repeated) specify an annotation that must exist on the resource
# Args:
#  [resource] the resource, eg clusterrole
#  [name] the resource name, eg, cluster-admin
# Example:
#  - Check that the namespace backend exists and has the label
#    "configmanagement.gke.io/marked=true"
#    resource::check ns backend -l "configmanagement.gke.io/marked=true"
#  - Check that the role pod-editor exists in the frontend namespace
#    resource::check -n frontend role pod-editor
function resource::check() {
  local arg
  local args=()
  local namespace=""
  local labels=()
  local annotations=()
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -n)
        namespace="${1:-}" # local
        shift
      ;;
      -l)
        labels+=("${1:-}")
        shift
      ;;
      -a)
        annotations+=("${1:-}")
        shift
      ;;
      *)
        args+=("$arg")
      ;;
    esac
  done

  local resource="${args[0]}"
  local name="${args[1]}"

  local cmd=("kubectl" "get" "$resource" "$name" -ojson)
  if [[ "$namespace" != "" ]]; then
    cmd+=(-n "$namespace")
  fi

  local output
  local status=0
  output=$("${cmd[@]}") || status=$?
  (( status == 0 )) || debug::error "Command" "${cmd[@]}" "failed, output ${output}"
  local json="$output"
  local key
  local value
  if [[ "${#labels[@]}" != 0 ]]; then
    local label
    for label in "${labels[@]}"; do
      key=$(sed -e 's/=.*//' <<< "$label")
      value=$(sed -e 's/[^=]*=//' <<< "$label")
      output=$(jq ".metadata.labels[\"${key}\"]" <<< "$json")
      if [[ "$output" != "\"$value\"" ]]; then
        debug::error "Expected label $value, got $output"
      fi
    done
  fi
  if [[ "${#annotations[@]}" != 0 ]]; then
    local annotation
    for annotation in "${annotations[@]}"; do
      key=$(sed -e 's/=.*//' <<< "$annotation")
      value=$(sed -e 's/[^=]*=//' <<< "$annotation")
      output=$(jq ".metadata.annotations[\"${key}\"]" <<< "$json")
      if [[ "$output" != "\"$value\"" ]]; then
        debug::error "Expected annotation $value, got $output"
      fi
    done
  fi
}

# Checks the count of a given resource
#
# Flags:
#  -n [namespace] get the count for a namespaced resource
#  -l [selector] use a selector during list
#  -r [resource] the resource, eg clusterrole
#  -c [count] the expected count, eg 5
function resource::check_count() {
  local args=()
  local count=""
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -c)
        count=${1:-}
        shift
      ;;
      *)
        args+=("$arg")
      ;;
    esac
  done
  [ -n "$count" ] || (echo "Must specify -c [count]"; return 1)

  local actual
  local status=0
  actual="$(resource::count "${args[@]}")" || status=$?
  if (( status != 0 )); then
    return 1
  fi
  if (( count != actual )); then
    echo -e "check_count failure\\nArgs: ${args[*]}\\nExpected $count, got $actual\\n"
    return 1
  fi
}

# Get the count of a given resource
#
# Flags:
#  -n [namespace] get the count for a namespaced resource
#  -l [selector] use a selector during list
#  -a [annotation] filter by annotation
#  -r [resource] the resource, eg clusterrole
function resource::count() {
  local namespace=""
  local selector=""
  local annotation=""
  local resource=""
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -n)
        namespace=${1:-}
        shift
      ;;
      -l)
        selector=${1:-}
        shift
      ;;
      -a)
        annotation=${1:-}
        shift
      ;;
      -r)
        resource=${1:-}
        shift
      ;;
      *)
        echo "Unexpected arg $arg" >&3
        return 1
      ;;
    esac
  done
  [ -n "$resource" ] || (echo "Must specify -r [resource]" >&3; return 1)

  local cmd=("kubectl" "get" "$resource" "-o" "json")
  if [[ "$namespace" != "" ]]; then
    cmd+=(-n "$namespace")
  fi
  if [[ "$selector" != "" ]]; then
    cmd+=(-l "$selector")
  fi

  local output
  local status=0
  output="$("${cmd[@]}")" || status=$?
  if (( status != 0 )); then
    debug::error "Command" "${cmd[@]}" "failed, output ${output}"
    return 1
  fi

  local count=0
  if [[ "$annotation" != "" ]]; then
    local key=""
    local value=""
    key=$(cut -d'=' -f1 <<<"${annotation}")
    value=$(cut -d'=' -f2 <<<"${annotation}")
    count=$(echo "$output" | jq -c ".items | select( .[].metadata.annotations.\"${key}\" == \"${value}\" )" | wc -l)
  else
    count=$(echo "$output" | jq '.items | length')
  fi

  echo "$count"
}

# Delete the given resources.
#
# Flags:
#  -n [namespace] the namespace the resource resides in
#  -l [selector] use a selector during list
#  -a [annotation] filter by annotation
#  -r [resource] the resource, eg clusterrole
function resource::delete() {
  local namespace=""
  local selector=""
  local annotation=""
  local resource=""
  while [[ $# -gt 0 ]]; do
    local arg="${1:-}"
    shift
    case $arg in
      -n)
        namespace=${1:-}
        shift
      ;;
      -l)
        selector=${1:-}
        shift
      ;;
      -a)
        annotation=${1:-}
        shift
      ;;
      -r)
        resource=${1:-}
        shift
      ;;
      *)
        echo "Unexpected arg $arg" >&3
        return 1
      ;;
    esac
  done
  [ -n "$resource" ] || (echo "Must specify -r [resource]" >&3; return 1)

  local cmd=("kubectl" "get" "$resource" "-o" "json")
  if [[ "$namespace" != "" ]]; then
    cmd+=(-n "$namespace")
  fi
  if [[ "$selector" != "" ]]; then
    cmd+=(-l "$selector")
  fi

  local output
  local status=0
  output="$("${cmd[@]}")" || status=$?
  if (( status != 0 )); then
    debug::error "Command" "${cmd[@]}" "failed, output ${output}"
    return 1
  fi

  local names
  names=""  # Assigning () makes it an unbound variable.
  if [[ "$annotation" != "" ]]; then
    local key=""
    local value=""
    key=$(cut -d'=' -f1 <<<"${annotation}")
    value=$(cut -d'=' -f2 <<<"${annotation}")
    mapfile -t names < <(echo "$output" | jq -r ".items[] | select( .metadata.annotations.\"${key}\" == \"${value}\" ) | .metadata.name")
  else
    mapfile -t names < <(echo "$output" | jq -r '.items[] | .metadata.name')
  fi
  if [ ${#names[@]} -eq 0 ]; then
    return 0
  fi

  if [[ "${resource}" == "namespace" \
        || "${resource}" == "namespaces" \
        || ${resource} == "ns" ]]; then
    # Remove "default" and "kube-system" from the list of resources to delete,
    # because they can't be removed.  This is very manual, but is apparently one
    # of the few recommended ways to do this in bash that I could understand.
    declare -a tmpnames
    for ns in "${names[@]}"; do
      if [[ "${ns}" != "default" && "${ns}" != "kube-system" && "${ns}" != safety* ]]; then

        if kubectl get ns "${ns}" | grep -q "Terminating"; then
          # Technically a race condition if a delete happens between here and
          # when we attempt to delete.
          debug::log "Namespace ${ns} is already terminating"
        else
          tmpnames+=("${ns}")
        fi
      fi

    done
    if [ -z "${tmpnames+x}" ]; then
      return 0
    fi
    if [ ${#tmpnames[@]} -eq 0 ]; then
      return 0
    fi
    names=("${tmpnames[@]}")
  fi

  local deletecmd=("kubectl" "delete" "$resource")
  if [[ "$namespace" != "" ]]; then
    deletecmd+=(-n "$namespace")
  fi

  deletecmd+=("${names[@]}")

  local output
  local status=0
  output="$("${deletecmd[@]}")" || status=$?
  if (( status != 0 )); then
    debug::error "Command" "${deletecmd[@]}" "failed, output ${output}"
    return 1
  fi
}
