#!/bin/bash

# Helpers for waiting for async events (e.g. Pods to come up).

# Waits for the command finish with a certain exit code.
#
# Wait for success:
#  wait:for -- <command>
# Wait for failure:
#  wait::for -f -- <command>
#
# Flags
#   -d [deadline]      The deadline in seconds since the epoch
#   -e [exit code]     Wait for specific integer exit code
#   -f                 Wait for failure (exit nonzero)
#   -o [output]        The expected exact output at the end of the wait. If omitted,
#                      output is not checked.  Default: ""
#   -c [output]        The expected substring of output ("contains") at the end
#                      of the wait. If omitted, output is not checked.
#                      Default: ""
#   -p [poll interval] The amount of time to wait between executions
#   -s                 Wait for success (exit 0)
#   -t [timeout]       The timeout in seconds (default 300 seconds)
#   -l                 Show timing for the command (1-second granularity)
#   -- End of flags, command starts after this
# Args
#  Args for command
function wait::for() {
  local args=()
  local sleeptime="0.1"
  local exitf=(wait::__exit_eq 0)
  local timeout=300
  local deadline="$(( $(date +%s) + timeout ))"
  local expected_output=""
  local contained_output=""
  local show_timing=false

  local parse_args=false
  for i in "$@"; do
    if [[ "$i" == "--" ]]; then
      parse_args=true
    fi
  done

  if $parse_args; then
    while [[ $# -gt 0 ]]; do
      local arg="${1:-}"
      shift
      case $arg in
        -s)
          exitf=(wait::__exit_eq 0)
        ;;
        -f)
          exitf=(wait::__exit_neq 0)
        ;;
        -e)
          exitf=(wait::__exit_eq "${1:-}")
          shift
        ;;
        -l)
          show_timing=true
        ;;
        -t)
          timeout="${1:-}"
          deadline=$(( $(date +%s) + timeout ))
          shift
        ;;
        -d)
          deadline=${1:-}
          timeout=$(( deadline - $(date +%s) ))
          shift
        ;;
        -p)
          sleeptime=${1:-}
          shift
        ;;
        -o)
          expected_output=${1:-}
          shift
        ;;
        -c)
          contained_output=${1:-}
          shift
        ;;
        --)
          break
        ;;
        *)
          echo "Unexpected arg $arg" >&3
          return 1
        ;;
      esac
    done
  fi
  args=("$@")

  local out=""
  local status=0
  local iterations=0
  local start_time=0
  start_time=$(date +%s)
  while (( $(date +%s) < deadline )); do
    status=0
    out="$("${args[@]}" 2>&1)" || status=$?
    if [[ -n "${expected_output}" ]] && [[ "${out}" != "${expected_output}" ]]; then
      sleep "${sleeptime}"
      continue
    fi
    if [[ -n "${contained_output}" ]] && [[ "${out}" != *"${contained_output}"* ]]; then
      sleep "${sleeptime}"
      continue
    fi
    if "${exitf[@]}" "${status}"; then
      if ${show_timing}; then
        local elapsed
        elapsed=$(( $(date +%s) - start_time ))
        # shellcheck disable=SC2145
        echo "> iterations:${iterations} elapsed:${elapsed}s command:${args[@]}" >&3
      fi
      return 0
    fi
    sleep "$sleeptime"
  done

  echo "Command timed out after ${timeout} sec:" \
    "${args[@]}" "status: ${status} last output: '${out}'"
  return 1
}

function wait::__exit_eq() {
  local lhs="${1:-}"
  local rhs="${2:-}"
  (( lhs == rhs ))
}

function wait::__exit_neq() {
  local lhs="${1:-}"
  local rhs="${2:-}"
  (( lhs != rhs ))
}

