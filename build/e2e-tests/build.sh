#!/bin/bash
# Copyright 2022 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


set -euo pipefail

quiet=false
tags=()
while (( $# != 0 )); do
  arg="${1:-}"
  shift
  case $arg in
    --quiet)
      quiet=true
      ;;

    -t)
      tags+=(-t "${1:-}")
      shift
      ;;

    *)
      echo "unknown arg $arg"
      exit 1
      ;;
  esac
done

echo "+++ Building intermediate e2e image"
docker build --target gcloud-install \
  --network host \
  -t e2e-tests-gcloud \
  "$(dirname "$0")"

GID="${GID:-$(id -g)}"

echo "+++ Building e2e image"
exec docker build \
  --network host \
  --build-arg "UID=${UID}" \
  --build-arg "GID=${GID}" \
  --build-arg "UNAME=${USER}" \
  "${tags[@]}" \
  "$(dirname "$0")"
