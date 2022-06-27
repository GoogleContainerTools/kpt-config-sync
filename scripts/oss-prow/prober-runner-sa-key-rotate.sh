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
# Our e2e job currently relies on a "prober cred" cert for talking to cloud.
# I'm not sure why it's called prober cred, but it is.  We have to create this
# in our prow cluster in order for e2e to work.
#
# The prober cred is a service account key, which expires every 90 days. This
# script rotates the SA key and sets up the nomos-prober-runner-gcp-client-key
# secret in the team prow cluster inside the `test-pods` namespace.
#
# Latest SA key can be accessed via:
# gcloud secrets versions access latest --secret nomos-prober-runner-gcp-client-key --project oss-prow-build-kpt-config-sync
#

set -euox pipefail

PROJECT_NAME=${PROJECT_NAME:-oss-prow-build-kpt-config-sync}
SERVICE_ACCOUNT=${SERVICE_ACCOUNT:-prober-runner}
KEY_FILE=${KEY_FILE:-prober_runner_client_key.json}
SECRET=${SECRET:-nomos-prober-runner-gcp-client-key}
GCP_PROBER_CREDS=${GCP_PROBER_CREDS:-/etc/prober-gcp-service-account/prober_runner_client_key.json}

# Create a new key if the previous one is older than 3 hours.
_THREE_HOURS=$(( 3 * 60 * 60 ))
KEY_MAX_AGE=${KEY_MAX_AGE:-${_THREE_HOURS}}

# GC the SA key after ~5 keys or so
_KEY_GC_AGE=$(( KEY_MAX_AGE * 5 ))

# Secret manager secret will live for 7 days.  Right now our keys last for 90
# days due to an exemption, so leaving them around for a while isn't a problem.
# Once our exemption is up, we will have 24 hour keys, so at that time we can
# lower this value.
TTL_SECS=$(( 7 * 24 * 60 * 60 ))

# SA email derived from service account and project env vars.
SERVICE_ACCOUNT_EMAIL=${SERVICE_ACCOUNT}@${PROJECT_NAME}.iam.gserviceaccount.com

function create_sa_key() {
  gcloud iam service-accounts keys create "${KEY_FILE}" \
    --iam-account "${SERVICE_ACCOUNT_EMAIL}" \
    --project "${PROJECT_NAME}"
}

function get_sa_key_secret() {
  echo "++++ Getting latest secret from ${PROJECT_NAME}/${SECRET}, writing to ${KEY_FILE}"
  gcloud secrets versions access latest \
    --secret "${SECRET}" \
    --project "${PROJECT_NAME}" \
    > "${KEY_FILE}"

  if [ ! -s  "${KEY_FILE}" ]; then
    echo "Failed to get service account key file Secret"
    exit 1
  fi
}

function update_k8s_secret() {
  local prow_test_ns="test-pods"
  local prow_cluster="prow"
  local prow_zone="us-central1-b"

  get_sa_key_secret

  # Connect to the Prow cluster to update the Kubernetes Secret.
  gcloud --quiet container clusters get-credentials "${prow_cluster}" --zone="${prow_zone}" --project="${PROJECT_NAME}"
  kubectl create secret generic "${SECRET}" \
    -n "${prow_test_ns}" \
    --save-config \
    --dry-run=client \
    --from-file "${KEY_FILE}" \
    -o yaml | \
    kubectl apply -f -
}

# Creates a new SA key then creates a gcp secret for the key.
function rotate_new_key() {
  # If no key exists, create one.
  if ! gcloud secrets describe "${SECRET}" --project="${PROJECT_NAME}" &> /dev/null; then
    echo "gcloud secret ${SECRET} does not exist, creating"
    create_sa_key
    gcloud secrets create "${SECRET}" \
      --data-file "${KEY_FILE}" \
      --project "${PROJECT_NAME}" \
      --ttl "${TTL_SECS}s" \
      --quiet
    update_k8s_secret
    return
  fi

  local key_create_time
  local key_create_unix_time
  local now
  key_create_time=$(gcloud secrets versions describe latest --project "${PROJECT_NAME}" --secret "${SECRET}" --format json \
    | jq -r '.createTime')
  key_create_unix_time=$(date -d "${key_create_time}" +%s)
  now=$(date +%s)
  if (( key_create_unix_time + KEY_MAX_AGE < now )); then
    echo "latest key older than $KEY_MAX_AGE seconds, creating new SA key"
    create_sa_key
    gcloud secrets versions add "${SECRET}" \
      --data-file "${KEY_FILE}" \
      --project "${PROJECT_NAME}"
    update_k8s_secret
  else
    echo "latest key newer than $KEY_MAX_AGE seconds, won't create new SA key"
  fi
}

# Removes old service account keys.
function clean_old_keys() {
  local remove_before_date
  local key_list
  remove_before_date=$(date --date="$_KEY_GC_AGE seconds ago" --iso-8601=seconds)
  key_list=$(gcloud iam service-accounts keys list \
    --format=json \
    --managed-by=user \
    --created-before="${remove_before_date}" \
    --iam-account "${SERVICE_ACCOUNT_EMAIL}" \
    --project="${PROJECT_NAME}")

  local -a keys
  mapfile -t keys < <(jq -r '.[].name' <<< "$key_list")
  local key
  for key in "${keys[@]}"; do
    echo "deleting key ${key} from service account ${SERVICE_ACCOUNT_EMAIL}"
    key_id=$(basename "${key}")
    gcloud iam service-accounts keys delete "${key_id}" \
      --quiet \
      --iam-account "${SERVICE_ACCOUNT_EMAIL}" \
      --project="${PROJECT_NAME}"
  done
}

# Makes the service account from ${GCP_PROBER_CREDS} the active account that drives cluster changes.
gcloud --quiet auth activate-service-account --key-file="${GCP_PROBER_CREDS}"

rotate_new_key
clean_old_keys
