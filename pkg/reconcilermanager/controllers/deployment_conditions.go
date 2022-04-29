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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// The set of status conditions which can be assigned to resources.
	statusInProgress statusType = "InProgress"
	statusFailed     statusType = "Failed"
	// currentStatus specifies that the resource has progressed successfully and available.
	statusCurrent statusType = "Current"
)

type statusType string

// status contains the deployment status and message.
type deploymentStatus struct {
	// status
	status statusType
	// message
	message string
}

// inProgressStatus creates a status result with the InProgress status.
func inProgressStatus(message string) *deploymentStatus {
	return &deploymentStatus{
		status:  statusInProgress,
		message: message,
	}
}

func checkDeploymentConditions(depObj *appsv1.Deployment) (*deploymentStatus, error) {
	available := false
	progressing := false

	for _, c := range depObj.Status.Conditions {
		switch c.Type {
		case appsv1.DeploymentProgressing:
			if c.Reason == "ProgressDeadlineExceeded" {
				return &deploymentStatus{
					status:  statusFailed,
					message: "Reconciler Deployment progress deadline exceeded",
				}, nil
			}
			if c.Status == corev1.ConditionTrue && c.Reason == "NewReplicaSetAvailable" {
				progressing = true
			}
		case appsv1.DeploymentAvailable:
			if c.Status == corev1.ConditionTrue {
				available = true
			}
		}
	}

	//replicas
	specReplicas := *depObj.Spec.Replicas
	statusReplicas := depObj.Status.Replicas

	if specReplicas > statusReplicas {
		message := fmt.Sprintf("Replicas: %d/%d", statusReplicas, specReplicas)
		return inProgressStatus(message), nil
	}

	updatedReplicas := depObj.Status.UpdatedReplicas
	if specReplicas > updatedReplicas {
		message := fmt.Sprintf("Updated: %d/%d", updatedReplicas, specReplicas)
		return inProgressStatus(message), nil
	}

	if statusReplicas > specReplicas {
		message := fmt.Sprintf("Pending termination: %d", statusReplicas-specReplicas)
		return inProgressStatus(message), nil
	}

	availableReplicas := depObj.Status.AvailableReplicas
	if updatedReplicas > availableReplicas {
		message := fmt.Sprintf("Available: %d/%d", availableReplicas, updatedReplicas)
		return inProgressStatus(message), nil
	}

	readyReplicas := depObj.Status.ReadyReplicas
	if specReplicas > readyReplicas {
		message := fmt.Sprintf("Ready: %d/%d", readyReplicas, specReplicas)
		return inProgressStatus(message), nil
	}

	// Check status conditions.
	if !progressing {
		return inProgressStatus("Reconciler ReplicaSet not Available"), nil
	}
	if !available {
		return inProgressStatus("Reconciler Deployment not Available"), nil
	}

	// All ok.
	return &deploymentStatus{
		status:  statusCurrent,
		message: fmt.Sprintf("Deployment is available. Replicas: %d", statusReplicas),
	}, nil
}
