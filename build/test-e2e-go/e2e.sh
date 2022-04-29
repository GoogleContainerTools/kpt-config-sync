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
# golang e2e test launcher. Do not run directly, this is intended to be executed by the prow job inside a container.

set -eo pipefail

DIR=$(dirname "${BASH_SOURCE[0]}")

if [ $KUBERNETES_ENV == "GKE" ]; then
  echo "GKE environment"

  # Makes the service account from ${gcp_prober_cred} the active account that drives
  # cluster changes.
  gcloud --quiet auth activate-service-account --key-file="${GCP_PROBER_CREDS}"

  # Installs gcloud as an auth helper for kubectl with the credentials that
  # were set with the service account activation above.
  # Needs cloud.containers.get permission.
  # shellcheck disable=SC2154
  if [[ -z ${GCP_ZONE} ]] && [[ -z ${GCP_REGION} ]]; then
    echo "neither GCP_ZONE nor GCP_REGION is set."
    exit 1
  elif [[ -n ${GCP_ZONE} ]] && [[ -n ${GCP_REGION} ]]; then
    echo "At most one of GCP_ZONE | GCP_REGION may be specified."
    exit 1
  elif [[ -n ${GCP_ZONE} ]]; then
    gcloud --quiet container clusters get-credentials "${GCP_CLUSTER}" --zone="${GCP_ZONE}" --project="${GCP_PROJECT}"
  else
    gcloud --quiet container clusters get-credentials "${GCP_CLUSTER}" --region="${GCP_REGION}" --project="${GCP_PROJECT}"
  fi

elif [ $KUBERNETES_ENV == "KIND" ]; then
  echo "kind environment"
else
  echo "Unexpected Kubernetes environment, '$KUBERNETES_ENV'"
  exit 1
fi

set +e

echo "Starting e2e tests"
start_time=$(date +%s)
go test ./e2e/... --e2e --test.v "$@" | tee test_results.txt
exit_code=$?
end_time=$(date +%s)
echo "Tests took $(( end_time - start_time )) seconds"

if [ -d "/logs/artifacts" ]; then
  echo "Creating junit xml report"
  cat test_results.txt | go-junit-report > /logs/artifacts/junit_report.xml
fi

exit $exit_code
