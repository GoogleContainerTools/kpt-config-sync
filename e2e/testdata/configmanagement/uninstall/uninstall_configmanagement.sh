#!/bin/bash
# Copyright 2024 Google LLC
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

# This script uninstalls ConfigManagement

set -euox pipefail

hnc_enabled="$(kubectl get configmanagements.configmanagement.gke.io config-management -o=jsonpath="{.spec.hierarchyController.enabled}" --ignore-not-found)"

if [[ "${hnc_enabled}" == "true" ]]; then
  echo "hierarchy Controller is enabled on the ConfigManagement object. It must be disabled before migrating."
  echo "This can be done by unsetting the spec.hierarchyController field on ConfigManagement."
  exit 1
fi

kubectl delete deployment -n config-management-system config-management-operator --ignore-not-found --cascade=foreground

if kubectl get configmanagement config-management &> /dev/null ; then
  kubectl patch configmanagement config-management --type="merge" -p '{"metadata":{"finalizers":[]}}'
  kubectl delete configmanagement config-management --cascade=orphan --ignore-not-found
fi

kubectl delete clusterrolebinding config-management-operator --ignore-not-found
kubectl delete clusterrole config-management-operator --ignore-not-found
kubectl delete serviceaccount -n config-management-system config-management-operator --ignore-not-found
kubectl delete customresourcedefinition configmanagements.configmanagement.gke.io --ignore-not-found
