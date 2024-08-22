#!/bin/bash
# Copyright 2023 Google LLC
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

#
# This command will generate the clientset for our custom resources.
#
# For clientset
# https://github.com/kubernetes/community/blob/master/contributors/devel/generating-clientset.md
#
# For deepcopy, there are no docs.
#
# If you want to generate a new CRD, create doc.go, types.go, register.go and
# use the existing files for a reference on all the // +[comment] things you
# need to add so it will work properly, then add it to INPUT_APIS
#
# Note that there are a bunch of other generators in
# k8s.io/code-generator/tree/master/cmd that we might have to use in the future.
#
# This script requires the repo to be in the GOPATH.
# Alternatively, make a symlink from the GOPATH package source directory to the
# repo directory.

set -euo pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
CODEGEN_PKG=${CODEGEN_PKG:-"${SCRIPT_ROOT}/vendor/k8s.io/code-generator"}

# Helper script is vendored from k8s.io/code-generator
# shellcheck source=vendor/k8s.io/code-generator/kube_codegen.sh
source "${CODEGEN_PKG}/kube_codegen.sh"

# Where to put the generated client set
GOMOD_NAME="$(grep "^module" "${SCRIPT_ROOT}/go.mod" | cut -d' ' -f2)"

kube::codegen::gen_helpers \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.txt" \
    "${SCRIPT_ROOT}/pkg/api"

kube::codegen::gen_client \
    --with-watch \
    --output-dir "${SCRIPT_ROOT}/pkg/generated" \
    --output-pkg "${GOMOD_NAME}/pkg/generated" \
    --boilerplate "${SCRIPT_ROOT}/hack/boilerplate.txt" \
    "${SCRIPT_ROOT}/pkg/api"
