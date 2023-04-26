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

# This script builds and pushes the test helm charts to GAR.
# The helm charts are built from the e2e/testdata/helm-charts folder, and used in the e2e test.
#
# Pre-requisite:
# 1. GCP_PROJECT is set.
# 2. The GAR repository has already been created in the ${GCP_PROJECT} project.
# 3. You have the write permission to the GAR repositories.

set -euo pipefail

REPO_DIR="$(readlink -f "$(dirname "$0")")/.."

if [ "${GCP_PROJECT:-"unset"}" == "unset" ]; then
  echo 'Error: GCP_PROJECT is unset. Please provide the GCP_PROJECT env variable to specify your GCP project' >&2
  exit 1
fi

LOCATION=us
AR_HELM_REPO=${LOCATION}-docker.pkg.dev/${GCP_PROJECT}/config-sync-test-ar-helm

HELM_CHARTS_DIR=${REPO_DIR}/e2e/testdata/helm-charts

COREDNS_CHART=coredns
COREDNS_CHART_VERSION=1.13.3
COREDNS_CHART_TAR=${COREDNS_CHART}-${COREDNS_CHART_VERSION}.tgz

gcloud auth application-default print-access-token | helm registry login -u oauth2accesstoken \
  --password-stdin "https://${LOCATION}-docker.pkg.dev"

pushd "${HELM_CHARTS_DIR}"

echo "Pushing the '${COREDNS_CHART}' chart to the '${AR_HELM_REPO}' repository"
tar -cvzf "${COREDNS_CHART_TAR}" "${COREDNS_CHART}"
helm push "${COREDNS_CHART_TAR}" "oci://${AR_HELM_REPO}"
rm -rf "${COREDNS_CHART_TAR}"

NS_CHART=ns-chart
NS_CHART_VERSION=0.1.0
NS_CHART_TAR=${NS_CHART}-${NS_CHART_VERSION}.tgz
echo "Pushing the '${NS_CHART}' chart to the '${AR_HELM_REPO}' repository"
tar -cvzf "${NS_CHART_TAR}" "${NS_CHART}"
helm push "${NS_CHART_TAR}" "oci://${AR_HELM_REPO}"
rm -rf "${NS_CHART_TAR}"

SIMPLE=simple
SIMPLE_VERSION=1.0.0
SIMPLE_TAR=${SIMPLE}-${SIMPLE_VERSION}.tgz
echo "Pushing the '${SIMPLE}' chart to the '${AR_HELM_REPO}' repository"
tar -cvzf "${SIMPLE_TAR}" "${SIMPLE}"
helm push "${SIMPLE_TAR}" "oci://${AR_HELM_REPO}"
rm -rf "${SIMPLE_TAR}"

popd
