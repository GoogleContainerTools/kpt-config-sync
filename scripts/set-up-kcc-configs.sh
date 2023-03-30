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

# This script sets up the test-infra for integration between Config Sync and KCC.
# The source configs are stored in the e2e/testdata/configsync-kcc folder, and used in the e2e/testcases/kcc_test.go test.
#
# Pre-requisite:
# 1. GCP_PROJECT, KCC_MANAGED_PROJECT, GCP_CLUSTER and GCP_ZONE are set.
# 2. The KCC_MANAGED_PROJECT project is created with the config-connector addon enabled.
# 3. The CSR repository is created in the ${GCP_PROJECT} project.
# 4. You have the write permission to the CSR repository.
# 5. You have permission to enable API service, manage IAM service accounts and policies in the ${KCC_MANAGED_PROJECT} project.

REPO_DIR="$(readlink -f "$(dirname "$0")")/.."

if [ "${GCP_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: GCP_PROJECT is unset. Please provide the GCP_PROJECT env variable to specify your GCP project' >&2
  exit 1
fi

if [ "${KCC_MANAGED_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: KCC_MANAGED_PROJECT is unset. Please provide the KCC_MANAGED_PROJECT env variable to specify your KCC-managed project' >&2
  exit 1
fi

if [ "${GCP_CLUSTER:-"unset"}" == "unset" ]; then
  echo 'Error: GCP_CLUSTER is unset. Please provide the GCP_CLUSTER env variable to specify your GCP cluster' >&2
  exit 1
fi

if [ "${GCP_ZONE:-"unset"}" == "unset" ]; then
  echo 'Error: GCP_ZONE is unset. Please provide the GCP_ZONE env variable to specify your cluster zone' >&2
  exit 1
fi

KCC_PACKAGE_NAME=configsync-kcc
KCC_DIR=${REPO_DIR}/e2e/testdata/${KCC_PACKAGE_NAME}

TMP_DIR=/tmp/${KCC_PACKAGE_NAME}
KCC_REPO_URL=https://source.developers.google.com/p/${GCP_PROJECT}/r/${KCC_PACKAGE_NAME}

echo "Publishing KCC resources to the CSR repo: ${KCC_REPO_URL}"
rm -rf "${TMP_DIR}" && cp -r "${KCC_DIR}" "${TMP_DIR}"
pushd "${TMP_DIR}"
git init
sed -i "s/KCC-MANAGED-PROJECT/${KCC_MANAGED_PROJECT}/g" "${TMP_DIR}/kcc/namespace.yaml"
sed -i "s/KCC-MANAGED-PROJECT/${KCC_MANAGED_PROJECT}/g" "${TMP_DIR}/kcc/policy-member.yaml"
git add -A && git commit -m "Add KCC resources"
git remote add google "${KCC_REPO_URL}"
git push --all -f google
popd

echo "Enable Cloud Resources Manager API"
gcloud services enable cloudresourcemanager.googleapis.com --project="${KCC_MANAGED_PROJECT}"

echo "Configuring IAM service account for KCC integration test"
SERVICE_ACCOUNT_NAME=kcc-integration

gcloud iam service-accounts create "${SERVICE_ACCOUNT_NAME}" --project="${KCC_MANAGED_PROJECT}" || true

gcloud projects add-iam-policy-binding "${KCC_MANAGED_PROJECT}" \
  --member="serviceAccount:${SERVICE_ACCOUNT_NAME}@${KCC_MANAGED_PROJECT}.iam.gserviceaccount.com" \
  --role="roles/editor"

gcloud projects add-iam-policy-binding "${KCC_MANAGED_PROJECT}" \
  --member="serviceAccount:${SERVICE_ACCOUNT_NAME}@${KCC_MANAGED_PROJECT}.iam.gserviceaccount.com" \
  --role="roles/iam.securityAdmin"

gcloud iam service-accounts add-iam-policy-binding \
  "${SERVICE_ACCOUNT_NAME}@${KCC_MANAGED_PROJECT}.iam.gserviceaccount.com" \
  --member="serviceAccount:${GCP_PROJECT}.svc.id.goog[cnrm-system/cnrm-controller-manager]" \
  --role="roles/iam.workloadIdentityUser" \
  --project="${KCC_MANAGED_PROJECT}"

echo "Configuring Config Connector on the CI cluster"
gcloud container clusters get-credentials "${GCP_CLUSTER}" --zone "${GCP_ZONE}" --project "${GCP_PROJECT}"
cat <<EOF | kubectl apply -f -
# configconnector.yaml
apiVersion: core.cnrm.cloud.google.com/v1beta1
kind: ConfigConnector
metadata:
  # the name is restricted to ensure that there is only one
  # ConfigConnector resource installed in your cluster
  name: configconnector.core.cnrm.cloud.google.com
spec:
 mode: cluster
 googleServiceAccount: "${SERVICE_ACCOUNT_NAME}@${KCC_MANAGED_PROJECT}.iam.gserviceaccount.com"
EOF
