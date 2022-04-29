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
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

var reconcilerDeploymentReplicaCount int32 = 1

func setReplicas(specReplicaCount, statusReplicaCount int32) depMutator {
	return func(dep *appsv1.Deployment) {
		dep.Spec.Replicas = &specReplicaCount

		// Status Replica.
		dep.Status.AvailableReplicas = statusReplicaCount
		dep.Status.Replicas = statusReplicaCount
		dep.Status.UpdatedReplicas = statusReplicaCount
		dep.Status.ReadyReplicas = statusReplicaCount
	}
}

func setStateConditions(progressCondition string, availableStatus corev1.ConditionStatus) depMutator {
	return func(dep *appsv1.Deployment) {
		// Conditions
		conditions := []appsv1.DeploymentCondition{
			{
				Type:   appsv1.DeploymentProgressing,
				Status: corev1.ConditionTrue,
				Reason: progressCondition,
			},
			{
				Type:   appsv1.DeploymentAvailable,
				Status: availableStatus,
			},
		}
		dep.Status.Conditions = append(dep.Status.Conditions, conditions...)
	}
}

func TestDeploymentConditions(t *testing.T) {
	testCases := []struct {
		name                 string
		reconcilerDeployment *appsv1.Deployment
		wantStatus           *deploymentStatus
		wantError            bool
	}{
		{
			name: "Deployment Available",
			reconcilerDeployment: repoSyncDeployment(nsReconcilerName,
				setReplicas(reconcilerDeploymentReplicaCount, reconcilerDeploymentReplicaCount),
				setStateConditions("NewReplicaSetAvailable", corev1.ConditionTrue),
			),
			wantStatus: &deploymentStatus{
				status:  statusCurrent,
				message: fmt.Sprintf("Deployment is available. Replicas: %d", reconcilerDeploymentReplicaCount),
			},
		},
		{
			name: "Deployment not available",
			reconcilerDeployment: repoSyncDeployment(nsReconcilerName,
				setReplicas(reconcilerDeploymentReplicaCount, reconcilerDeploymentReplicaCount),
				setStateConditions("NewReplicaSetAvailable", corev1.ConditionFalse),
			),
			wantStatus: &deploymentStatus{
				status:  statusInProgress,
				message: "Reconciler Deployment not Available",
			},
		},
		{
			name: "Not enough replicas available",
			reconcilerDeployment: repoSyncDeployment(nsReconcilerName,
				setReplicas(2, reconcilerDeploymentReplicaCount),
				setStateConditions("Reconciler ReplicaSet not Available", corev1.ConditionTrue),
			),
			wantStatus: &deploymentStatus{
				status:  statusInProgress,
				message: fmt.Sprintf("Replicas: %d/%d", reconcilerDeploymentReplicaCount, 2),
			},
		},
		{
			name: "Deployment progress deadline exceeded",
			reconcilerDeployment: repoSyncDeployment(nsReconcilerName,
				setReplicas(reconcilerDeploymentReplicaCount, 0),
				setStateConditions("ProgressDeadlineExceeded", corev1.ConditionFalse),
			),
			wantStatus: &deploymentStatus{
				status:  statusFailed,
				message: "Reconciler Deployment progress deadline exceeded",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotResult, err := checkDeploymentConditions(tc.reconcilerDeployment)
			if tc.wantError && err == nil {
				t.Errorf("deploymentConditions() got error: %q, want error", err)
			} else if !tc.wantError && err != nil {
				t.Errorf("deploymentConditions() got error: %q, want error: nil", err)
			}
			if !tc.wantError {
				if !cmp.Equal(gotResult, tc.wantStatus, cmp.AllowUnexported(deploymentStatus{})) {
					t.Errorf("deploymentConditions() got result: %v, want result: %v", gotResult, tc.wantStatus)
				}
			}
		})
	}
}
