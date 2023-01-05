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

# This script sets up the test-infra for validating the authentication via workload identity.
#
# Pre-requisite:
# 1. GCP_PROJECT, PROW_PROJECT, and FLEET_HOST_PROJECT are set.
# 2. The FLEET_HOST_PROJECT project is created.
# 4. You have the permission to enable API service in the ${GCP_PROJECT} project.
# 5. You have permission to manage IAM service accounts and policies in the ${GCP_PROJECT}, ${PROW_PROJECT}, and ${FLEET_HOST_PROJECT} projects.

if [ "${GCP_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: GCP_PROJECT is unset. Please provide the GCP_PROJECT env variable to specify your GCP project' >&2
  exit 1
fi

if [ "${FLEET_HOST_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: FLEET_HOST_PROJECT is unset. Please provide the FLEET_HOST_PROJECT env variable to specify your fleet host project' >&2
  exit 1
fi

# PROW_PROJECT is the project where the prow build clusters
if [ "${PROW_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: PROW_PROJECT is unset. Please provide the PROW_PROJECT env variable to specify your prow project' >&2
  exit 1
fi

echo "Enable Cloud Resources Manager API for project '${GCP_PROJECT}'"
gcloud services enable cloudresourcemanager.googleapis.com --project="${GCP_PROJECT}"

echo "Creating the 'gcp-sa-gkehub' service account"
gcloud beta services identity create --service=gkehub.googleapis.com --project="${FLEET_HOST_PROJECT}"

echo "Granting the gkehub service account the 'roles/gkehub.serviceAgent' role in both projects"
FLEET_HOST_PROJECT_NUMBER=$(gcloud projects describe "${FLEET_HOST_PROJECT}" --format "value(projectNumber)")
gcloud projects add-iam-policy-binding "${FLEET_HOST_PROJECT}" \
  --member "serviceAccount:service-${FLEET_HOST_PROJECT_NUMBER}@gcp-sa-gkehub.iam.gserviceaccount.com" \
  --role roles/gkehub.serviceAgent
gcloud projects add-iam-policy-binding "${GCP_PROJECT}" \
  --member "serviceAccount:service-${FLEET_HOST_PROJECT_NUMBER}@gcp-sa-gkehub.iam.gserviceaccount.com" \
  --role roles/gkehub.serviceAgent

echo "Granting the e2e-test-csr-reader service account the 'roles/iam.workloadIdentityUser' role for the '${FLEET_HOST_PROJECT}' project"
gcloud iam service-accounts add-iam-policy-binding --project="${GCP_PROJECT}" \
		--role roles/iam.workloadIdentityUser \
		--member="serviceAccount:${FLEET_HOST_PROJECT}.svc.id.goog[config-management-system/root-reconciler]" \
		"e2e-test-csr-reader@${GCP_PROJECT}.iam.gserviceaccount.com"

echo "Granting the e2e-test-ar-reader service account the 'roles/iam.workloadIdentityUser' role for the '${FLEET_HOST_PROJECT}' project"
gcloud iam service-accounts add-iam-policy-binding --project="${GCP_PROJECT}" \
		--role roles/iam.workloadIdentityUser \
		--member="serviceAccount:${FLEET_HOST_PROJECT}.svc.id.goog[config-management-system/root-reconciler]" \
		"e2e-test-ar-reader@${GCP_PROJECT}.iam.gserviceaccount.com"

echo "Granting the e2e-test-gcr-reader service account the 'roles/iam.workloadIdentityUser' role for the '${FLEET_HOST_PROJECT}' project"
gcloud iam service-accounts add-iam-policy-binding --project="${GCP_PROJECT}" \
		--role roles/iam.workloadIdentityUser \
		--member="serviceAccount:${FLEET_HOST_PROJECT}.svc.id.goog[config-management-system/root-reconciler]" \
		"e2e-test-gcr-reader@${GCP_PROJECT}.iam.gserviceaccount.com"

echo "Granting the e2e-test-runner service account of the {GCP_PROJECT} project the 'roles/gkehub.admin' role for the '${FLEET_HOST_PROJECT}' project"
gcloud projects add-iam-policy-binding "${FLEET_HOST_PROJECT}" \
  --member "serviceAccount:e2e-test-runner@${PROW_PROJECT}.iam.gserviceaccount.com" \
  --role roles/gkehub.admin
