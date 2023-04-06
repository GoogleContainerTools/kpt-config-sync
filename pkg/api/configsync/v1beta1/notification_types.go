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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Commit",type="string",JSONPath=".status.commit"
// +kubebuilder:printcolumn:name="LastUpdate",type="date",JSONPath=".status.LastUpdate"
// +kubebuilder:storageversion

// Notification represents the notification configuration for a RootSync or RepoSync
// object. It is created and managed by Config Sync.
type Notification struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// +optional
	Status NotificationStatus `json:"status,omitempty"`
}

// NotificationStatus represents the status of the notification controller for a
// RootSync or RepoSync with notifications enabled.
type NotificationStatus struct {
	// observedGeneration is the most recent generation observed for the RootSync/RepoSync resource.
	// It corresponds to the RootSync/RepoSync generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// hash of the source of truth that is rendered.
	// It can be a git commit hash, or an OCI image digest.
	// +optional
	Commit string `json:"commit,omitempty"`
	// lastUpdate is the timestamp of when this status was last updated by a
	// reconciler.
	// +optional
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`
	// errors is a list of any errors that occurred while sending notifications
	// from the change indicated by commit.
	// +optional
	Errors []NotificationError `json:"errors,omitempty"`
	// warnings is a list of any warnings that occurred while sending notifications
	// from the change indicated by commit.
	// +optional
	Warnings []NotificationError `json:"warnings,omitempty"`
	// deliveries is a list of notifications which were delivered for the change
	// indicated by commit.
	// +optional
	Deliveries []NotificationDelivery `json:"deliveries,omitempty"`
}

// NotificationDelivery represents a notification that was successfully delivered
type NotificationDelivery struct {
	// trigger is the name of the trigger which caused this notification delivery
	// to be sent.
	// +optional
	Trigger string `json:"trigger,omitempty"`
	// service is the service that the notification delivery is sent to.
	// For example github, slack, etc.
	// +optional
	Service string `json:"service,omitempty"`
	// recipient is the recipient that the notification delivery is sent to.
	// For example, this could be an email address for a notification with service of type email.
	// +optional
	Recipient string `json:"recipient,omitempty"`
	// alreadyNotified indicates that this notification was already delivered in a previous iteration
	// +optional
	AlreadyNotified bool `json:"alreadyNotified,omitempty"`
}

// NotificationError represents an error which occurred within the notification
// controller for a RootSync or RepoSync.
type NotificationError struct {
	// errorMessage describes the error that occurred.
	// +optional
	ErrorMessage string `json:"errorMessage"`
}

// +kubebuilder:object:root=true

// NotificationList contains a list of Notification
type NotificationList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Notification `json:"items"`
}
