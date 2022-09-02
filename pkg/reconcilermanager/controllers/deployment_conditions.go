// Copyright 2022 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package controllers

import (
	appsv1 "k8s.io/api/apps/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
)

// ComputeDeploymentStatus uses kstatus to compute the deployment status based
// on its conditions and other status fields.
func ComputeDeploymentStatus(depObj *appsv1.Deployment) (*kstatus.Result, error) {
	uObj, err := kinds.ToUnstructured(depObj, core.Scheme)
	if err != nil {
		return nil, err
	}

	result, err := kstatus.Compute(uObj)
	if err != nil {
		return nil, err
	}
	return result, nil
}
