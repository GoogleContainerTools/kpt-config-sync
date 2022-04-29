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
# e2e test launcher.  Do not run directly, this is intended to be executed by
# the Makefile for the time being.
#

set -exuo pipefail

[ -z "${GOTOPT2_BINARY}" ] && \
  (echo "environment is missing the gotopt2 binary"; exit 1)
readonly gotopt2_output=$(${GOTOPT2_BINARY} "${@}" <<EOF
flags:
- name: "e2e-container"
  type: string
  help: "The container used as the test fixture environment"
  default: "gcr.io/stolos-dev/e2e-tests:test-e2e-latest"
- name: "TEMP_OUTPUT_DIR"
  type: string
  help: "The directory for temporary output"
- name: "OUTPUT_DIR"
  type: string
  help: "The directory for temporary output"
- name: "mounted-prober-cred"
  type: string
  help: "If set, we will mount the prober creds path into the test runner from here"
- name: "hermetic"
  type: bool
  help: "If set, the runner will refrain from importing any files from outside of the container that uses it"
- name: "use-ephemeral-cluster"
  type: bool
  help: "If set, the test runner will start an ephemeral in-docker cluster to run the tests on"
- name: "fetch-prober-cred"
  type: bool
  help: "If set, we will fetch the service account key file from the cloud secret manager.  This is useful in hermetic tests where one can not rely on the credentials being mounted into the container"
- name: "prober-cred-secret"
  type: string
  default: "nomos-prober-runner-gcp-client-key"
  help: "The secret name for the service account key file stored in the cloud secret manager."
- name: "prober-cred-secret-project"
  type: string
  default: "stolos-dev"
  help: "The project in which the secret for the service account key file was created"
EOF
)
eval "${gotopt2_output}"

# TODO: remove the need to disable lint checks here.
# shellcheck disable=SC2154
readonly TEMP_OUTPUT_DIR="${gotopt2_TEMP_OUTPUT_DIR}"
# shellcheck disable=SC2154
readonly OUTPUT_DIR="${gotopt2_OUTPUT_DIR}"
# shellcheck disable=SC2154
readonly fetch_prober_cred="${gotopt2_fetch_prober_cred:-false}"
# shellcheck disable=SC2154
readonly mounted_prober_cred="${gotopt2_mounted_prober_cred}"
# shellcheck disable=SC2154
readonly hermetic="${gotopt2_hermetic:-false}"
# shellcheck disable=SC2154
readonly prober_cred_secret="${gotopt2_prober_cred_secret}"
# shellcheck disable=SC2154
readonly prober_cred_secret_project="${gotopt2_prober_cred_secret_project}"

if [[ "$OUTPUT_DIR" == "" ]]; then
  pushd "$(readlink -f "$(dirname "$0")/..")" > /dev/null
  OUTPUT_DIR="$(make print-OUTPUT_DIR)"
  popd > /dev/null
fi
if [[ "$TEMP_OUTPUT_DIR" == "" ]]; then
  TEMP_OUTPUT_DIR="$OUTPUT_DIR/tmp"
fi

echo "+++ Environment: "
env

DOCKER_FLAGS=()
if [[ "${mounted_prober_cred}" != "" ]]; then
  echo "+++ Mounting creds from: ${mounted_prober_cred}"
  DOCKER_FLAGS+=(-v "${mounted_prober_cred}:${mounted_prober_cred}")
fi

# Old-style podutils for go/prow use ${WORKSPACE} as base directory but do not
# define $ARTIFACTS.
if [[ -n "${WORKSPACE+x}" && -z "${ARTIFACTS+x}" ]]; then
  ARTIFACTS="${WORKSPACE}/_artifacts"
  echo "+++ Got legacy artifacts directory from workspace: ${ARTIFACTS}"
fi

# The ARTIFACTS env variable has the name of the directory that the test artifacts
# should be written to.  If one is defined, propagate it into the tester.
if [ -n "${ARTIFACTS+x}" ]; then
  echo "+++ Artifacts directory: ${ARTIFACTS}"
  DOCKER_FLAGS+=(
    -e "ARTIFACTS=${ARTIFACTS}"
    -v "${ARTIFACTS}:${ARTIFACTS}"
  )
fi

# This is the directory path to user's home inside the container.
HOME_IN_CONTAINER="${HOME}"
# This is the directory path to users' directory outside docker.
USER_DIR_ON_HOST="${HOME}"

if "${hermetic}"; then
  # In hermetic mode, the e2e tests do most of the setup required to connect to
  # the test environment.
  #
  # gcloud and kubectl are configured based on the credentials provided by the
  # test runner.   The credentials are either mounted into the container (if
  # the test runner is based on Kubernetes), in which case they are expected in
  # ${TEMP_OUTPUT_DIR}/config/... (see above), or downloaded from Secret Manager
  # using the credentials of the user that is running this wrapper using the flag
  # --fetch-prober-cred if the test runner is based off of local file content.
  echo "+++ Executing e2e tests in hermetic mode."

  USER_DIR_ON_HOST="${TEMP_OUTPUT_DIR}/user"
  rm -rf "${USER_DIR_ON_HOST}"
  mkdir -p "${USER_DIR_ON_HOST}"

  # Place the user's home in a writable directory.
  HOME_IN_CONTAINER="/tmp/user"
  DOCKER_FLAGS+=(
    -e "HOME=${HOME_IN_CONTAINER}"
    # Make the currently checked out directory available.
    -e "NOMOS_REPO=/tmp/nomos"
    -v "$(pwd):/tmp/nomos"
  )
else
  # Copy the gcloud and kubectl configuration into a separate directory then
  # update the gcloud auth provider path to point to the gcloud in the e2e image
  # and add mount points for gcloud / kubectl that will be sandboxed to the
  # container.  Use rsync for copying since we want to ensure the file mods are
  # preserved as to not expose auth tokens.
  #
  # A nice side effect of this is that you can switch context on gcloud / kubectl
  # while tests are running without issues.
  #
  echo "+++ Executing e2e tests in non-hermetic mode."
  rm -rf "$TEMP_OUTPUT_DIR/config"
  mkdir -p "$TEMP_OUTPUT_DIR/config/.kube"
  rsync -a "${HOME}/.kube" "$TEMP_OUTPUT_DIR/config"
  rsync -a "${HOME}/.config/gcloud" "$TEMP_OUTPUT_DIR/config"
  sed -i -e \
    's|cmd-path:.*gcloud$|cmd-path: /opt/gcloud/google-cloud-sdk/bin/gcloud|' \
    "$TEMP_OUTPUT_DIR/config/.kube/config"
  DOCKER_FLAGS+=(
    -v "${TEMP_OUTPUT_DIR}/config/.kube:${HOME_IN_CONTAINER}/.kube"
    -v "${TEMP_OUTPUT_DIR}/config/gcloud:${HOME_IN_CONTAINER}/.config/gcloud"
    -e "HOME=${HOME_IN_CONTAINER}"
    -v "${HOME}:${HOME_IN_CONTAINER}"
    -e "NOMOS_REPO=$(pwd)"
  )
fi

if ${gotopt2_use_ephemeral_cluster:-false}; then

  # Create one cluster named "kind".  Each cluster created this way gets a
  # separate kubeconfig file.
  readonly ephemeral_cluster_name="kind"
  kind delete cluster --loglevel=debug || true
  # https://github.com/kubernetes-sigs/kind/issues/426
  echo "kind" >./product_name
  cat <<EOF > "./kind-config.yaml"
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraMounts:
  - containerPath: /sys/class/dmi/id/product_name
    hostPath: ${PWD}/product_name
EOF
  kind create cluster \
    --config=./kind-config.yaml \
    --retain \
    --name="${ephemeral_cluster_name}" \
    --wait=120s --loglevel=debug || kind export logs "${ARTIFACTS}/kind-logs"

  DOCKER_FLAGS+=(
    "--network=host"
  )

  if ${hermetic}; then
    mkdir -p "${USER_DIR_ON_HOST}/.config/kind"
    kind get kubeconfig > \
      "${USER_DIR_ON_HOST}/.config/kind/${ephemeral_cluster_name}.kubeconfig"
    DOCKER_FLAGS+=(
      -e "KUBECONFIG=${HOME_IN_CONTAINER}/.config/kind/${ephemeral_cluster_name}.kubeconfig"
    )
  else
    mkdir -p "${TEMP_OUTPUT_DIR}/config/kind"
    kind get kubeconfig > \
      "${TEMP_OUTPUT_DIR}/config/kind/${ephemeral_cluster_name}.kubeconfig"
    DOCKER_FLAGS+=(
      -e "KUBECONFIG=${TEMP_OUTPUT_DIR}/config/kind/${ephemeral_cluster_name}.kubeconfig"
    )
  fi

  # Bootstrap credentials into the kind cluster here, as this is the last spot
  # we have access to the 'docker' binary.  Borrowed from
  # https://kind.sigs.k8s.io/docs/user/private-registries/

  # Create a temp dir for the docker config.
  echo "Creating temporary docker client config directory ..."
  DOCKER_CONFIG=$(mktemp -d)
  export DOCKER_CONFIG
  trap 'echo "Removing ${DOCKER_CONFIG}/*" && rm -rf ${DOCKER_CONFIG:?}' EXIT

  # This will reveal the access token of the current user to
  # the docker container.  Don't use this with your own account.
  gcloud auth print-access-token \
    | docker login -u oauth2accesstoken --password-stdin https://gcr.io
  for node in $(kind get nodes --name "${ephemeral_cluster_name}"); do
    readonly node_name="${node#node/}"
    docker cp "${DOCKER_CONFIG}/config.json" "${node_name}:/var/lib/kubelet/config.json"
    docker exec "${node_name}" systemctl restart kubelet.service || true
    echo "finished access token setup for node: ${node_name}"
  done
fi

if "${fetch_prober_cred}"; then
  key_file="${TEMP_OUTPUT_DIR}/config/prober_runner_client_key.json"
  echo "++++ Getting latest secret from ${prober_cred_secret_project}/${prober_cred_secret}, writing to ${key_file}"
  gcloud secrets versions access latest \
    --secret "${prober_cred_secret}" \
    --project "${prober_cred_secret_project}" \
    > "${key_file}"
fi

DOCKER_FLAGS+=(
    -u "$(id -u):$(id -g)"
    -v "${TEMP_OUTPUT_DIR}:/tmp"
    -v "${OUTPUT_DIR}/nomos":/opt/testing/nomos
    -v "${OUTPUT_DIR}/go/bin":/opt/testing/go/bin
    "${gotopt2_e2e_container}"
)

# shellcheck disable=SC2154
docker run "${DOCKER_FLAGS[@]}" "/opt/testing/nomos/e2e/setup.sh" "${gotopt2_args__[@]}"
