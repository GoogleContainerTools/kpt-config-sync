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

set -euox pipefail

NOMOS_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

# The tool doesn't gracefully handle multiple GOPATH values, so this will get
# the first and last values from GOPATH.
GOBASE="${GOPATH//:.*/}"
GOWORK="${GOPATH//.*:/}"
REPO="kpt.dev/configsync"

# Comma separted list of APIs to generate for clientset.
INPUT_BASE="${REPO}/pkg/api"
INPUT_APIS=$(
  find "$NOMOS_ROOT/pkg/api" -mindepth 2 -maxdepth 2 -type d \
  | sed -e "s|^$NOMOS_ROOT/pkg/api/||" \
  | tr '\n' ',' \
  | sed -e 's/,$//' \
)
echo "Found input APIs: ${INPUT_APIS}"

# Where to put the generated client set
OUTPUT_BASE="${GOPATH}/src"
OUTPUT_CLIENT="${REPO}/clientgen"

LOGGING_FLAGS=${LOGGING_FLAGS:- --logtostderr -v 5}
if ${SILENT:-false}; then
  LOGGING_FLAGS=""
fi

tools=()
for tool in client-gen deepcopy-gen informer-gen lister-gen; do
  tools+=("k8s.io/code-generator/cmd/${tool}")
done

# This should match the k8s.io/code-generator version in go.mod.
tag="v0.22.2"
echo "Checking out k8s.io/code-generator at tag ${tag}"
  # As of go 1.16, go install is the recommended way to build/install modules
  # https://go.dev/doc/go1.16#go-command
  for tool in "${tools[@]}"; do
    go install "${tool}@${tag}"
  done

# If we run go mod tidy, it removes the empty code-generator declaration from
# modules.txt which makes code generation fail silently. Forcing the empty
# vendor declaration with this resolves the issue, but it isn't clear why.
go mod vendor

echo "Using GOPATH base ${GOBASE}"
echo "Using GOPATH work ${GOWORK}"

for i in apis informer listers; do
  echo "Removing ${NOMOS_ROOT}/clientgen/$i"
  rm -rf "${NOMOS_ROOT}/clientgen/$i"
done

echo "Generating APIs"
"${GOBASE}/bin/client-gen" \
  --input-base "${INPUT_BASE}" \
  --input="${INPUT_APIS}" \
  --clientset-name="apis" \
  --output-base="${OUTPUT_BASE}" \
  --clientset-path "${OUTPUT_CLIENT}" \
  --go-header-file="hack/boilerplate.txt"

informer_inputs=""
for api in $(echo "${INPUT_APIS}" | tr ',' ' '); do
  if [[ "$informer_inputs" != "" ]]; then
    informer_inputs="$informer_inputs,"
  fi
  informer_inputs="${informer_inputs}${INPUT_BASE}/${api}"
done

echo "informer"
"${GOBASE}/bin/informer-gen" \
  ${LOGGING_FLAGS} \
  --input-dirs="${informer_inputs}" \
  --versioned-clientset-package="${OUTPUT_CLIENT}/apis" \
  --listers-package="${OUTPUT_CLIENT}/listers" \
  --output-base="$GOWORK/src" \
  --output-package="${OUTPUT_CLIENT}/informer" \
  --go-header-file="hack/boilerplate.txt" \
  --single-directory

echo "deepcopy"
# Creates types.generated.go
"${GOBASE}/bin/deepcopy-gen" \
  ${LOGGING_FLAGS} \
  --input-dirs="${informer_inputs}" \
  --output-file-base="types.generated" \
  --output-base="${OUTPUT_BASE}" \
  --go-header-file="hack/boilerplate.txt"

echo "lister"
"${GOBASE}/bin/lister-gen" \
  ${LOGGING_FLAGS} \
  --input-dirs="${informer_inputs}" \
  --output-base="$GOWORK/src" \
  --output-package="${OUTPUT_CLIENT}/listers" \
  --go-header-file="hack/boilerplate.txt"

echo "Generation Completed!"
