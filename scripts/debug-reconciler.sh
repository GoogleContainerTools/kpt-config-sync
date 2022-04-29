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
# Set up k8s cluster and git-sync for debuging the reconciler.
#

set -euo pipefail

nomos=$(dirname "$0")/..

# Params with sensible defaults
repo=https://github.com/GoogleCloudPlatform/csp-config-management.git
repodir=/tmp/repo
branch=1.0.0

# You can put your own defaults for these flags in debugger.defaults.bash
if [ -e ./debugger.defaults.bash ]; then
  # shellcheck disable=SC1091
  source ./debugger.defaults.bash
fi

function usage() {
    cat <<EOF
$(basname "$0")

Flags:
  --repo
    Have git-sync pull from this repo.
    If you want to use a repo on the local filesystem (recommended), specify
    the flag as something like:
      file:///usr/local/google/home/${USER}/repos/csp-config-management
    Default: ${repo}

  --branch
    Have git-sync check out this branch.
    Default: ${branch}

  --repodir
    The local directory to use for git-sync.
    IMPORTANT NOTE: git-sync will rm -rf EVERYTHING in this directory, do not
    use one with anything you care about if you are setting it.
    Default: ${repodir}

debugger.defaults.bash:
  Set the default flag values by creating the debugger.defaults.bash file in
  the root of your nomos repo.
EOF
  exit 0
}

for arg in "${@}"; do
  case $arg in
    -h|--help)
    usage
    ;;
  esac
done

while (( 0 < $# )); do
  arg=${1:-}
  shift
  case $arg in
    --branch)
      branch=${1:-}
      shift
    ;;
    --repodir)
      repodir=${1:-}
      shift
    ;;
    --repo)
      repo=${1:-}
      shift
    ;;
    *)
    echo "Invalid arg $arg, --help for options."
    exit 1
    ;;
  esac
done

# Installs the required k8s resources to run the reconciler and runs git-sync
function run() {
  files=(
    "$nomos/manifests/test-resources/00-namespace.yaml"
    "$nomos/manifests/namespace-selector-crd.yaml"
    "$nomos/manifests/cluster-selector-crd.yaml"
    "$nomos/manifests/cluster-registry-crd.yaml"
    "$nomos/manifests/reposync-crd.yaml"
    "$nomos/manifests/rootsync-crd.yaml"
    # NOTE: these are only used for status from the reconciler
    "$nomos/e2e/testdata/reconciler-manager/rootsync-sample.yaml"
    "$nomos/manifests/templates/reconciler-manager/dev.yaml"
    "$nomos/e2e/testdata/reconciler-manager/reposync-sample.yaml"
  )

  local -a args
  args=()
  for f in "${files[@]}"; do
    args+=(-f "$f")
  done
  kubectl apply "${args[@]}"
  if ! kubectl get clusterrolebinding "cluster-admin-$USER" &> /dev/null; then
    kubectl create clusterrolebinding "cluster-admin-$USER" \
      --clusterrole=cluster-admin --user="$USER@google.com"
  fi

  cat <<EOF

----- Configure the Debugger -----

Configure IntelliJ to debug cmd/reconciler/main.go with the following params:

Root Reconciler:
  -git-dir ${repodir}/rev -scope ":root" -source-format [desired source format] -sync-dir [desired sync dir]

Namespace Reconciler (bookinfo namespace)
  -git-dir ${repodir}/rev -scope "bookinfo" -sync-dir [desired sync dir]

NOTE: not all flags are specified for reconciler, some annotations applied may be inaccurate.

----- Starting git-sync -----

EOF

  git-sync \
    -logtostderr \
    -branch "$branch" \
    -dest rev \
    -repo "$repo" \
    -root "$repodir" \
    -wait 1
}

run
