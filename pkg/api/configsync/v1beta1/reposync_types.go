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
// +kubebuilder:printcolumn:name="RenderingCommit",type="string",JSONPath=".status.rendering.commit"
// +kubebuilder:printcolumn:name="RenderingErrorCount",type="integer",JSONPath=".status.rendering.errorSummary.totalCount"
// +kubebuilder:printcolumn:name="SourceCommit",type="string",JSONPath=".status.source.commit"
// +kubebuilder:printcolumn:name="SourceErrorCount",type="integer",JSONPath=".status.source.errorSummary.totalCount"
// +kubebuilder:printcolumn:name="SyncCommit",type="string",JSONPath=".status.sync.commit"
// +kubebuilder:printcolumn:name="SyncErrorCount",type="integer",JSONPath=".status.sync.errorSummary.totalCount"
// +kubebuilder:storageversion

// RepoSync is the Schema for the reposyncs API
type RepoSync struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec RepoSyncSpec `json:"spec,omitempty"`
	// +optional
	Status RepoSyncStatus `json:"status,omitempty"`
}

// RepoSyncSpec defines the desired state of a RepoSync.
type RepoSyncSpec struct {
	// sourceFormat specifies how the repository is formatted.
	// See documentation for specifics of what these options do.
	//
	// Must be unstructured. Optional. Set to
	// unstructured if not specified.
	//
	// The validation of this is case-sensitive.
	// +kubebuilder:validation:Pattern=^(unstructured|)$
	// +optional
	SourceFormat string `json:"sourceFormat,omitempty"`

	// sourceType specifies the type of the source of truth.
	//
	// Must be one of git, oci. Optional. Set to git if not specified.
	// +kubebuilder:validation:Pattern=^(git|oci|)$
	// +kubebuilder:default:=git
	// +optional
	SourceType string `json:"sourceType,omitempty"`

	// git contains configuration specific to importing resources from a Git repo.
	// +optional
	*Git `json:"git,omitempty"`

	// oci contains configuration specific to importing resources from an OCI package.
	// +optional
	Oci *Oci `json:"oci,omitempty"`

	// override allows to override the settings for a namespace reconciler.
	// +nullable
	// +optional
	Override OverrideSpec `json:"override,omitempty"`
}

// RepoSyncStatus defines the observed state of a RepoSync.
type RepoSyncStatus struct {
	Status `json:",inline"`

	// conditions represents the latest available observations of the RepoSync's
	// current state.
	// +optional
	Conditions []RepoSyncCondition `json:"conditions,omitempty"`
}

// RepoSyncConditionType is an enum of types of conditions for RepoSyncs.
type RepoSyncConditionType string

// These are valid conditions of a RepoSync.
const (
	// The following conditions are currently recommended as "standard" resource
	// conditions which are supported by kstatus and kpt:
	// https://github.com/kubernetes-sigs/cli-utils/tree/master/pkg/kstatus#conditions

	// RepoSyncReconciling means that the RepoSync's spec has not yet been fully
	// reconciled/handled by the RepoSync controller.
	RepoSyncReconciling RepoSyncConditionType = "Reconciling"
	// RepoSyncStalled means that the RepoSync controller has not been able to
	// make progress towards reconciling the RepoSync.
	RepoSyncStalled RepoSyncConditionType = "Stalled"
	// RepoSyncSyncing means that the namespace reconciler is processing a hash (git commit hash or OCI image digest).
	RepoSyncSyncing RepoSyncConditionType = "Syncing"
)

// ErrorSource indicates the origination of errors.
type ErrorSource string

const (
	// RenderingError indicates the errors are from the `status.rendering.errors` field.
	RenderingError ErrorSource = "status.rendering.errors"
	// SourceError indicates the errors are from the `status.source.errors` field.
	SourceError ErrorSource = "status.source.errors"
	// SyncError indicates the errors are from the `status.sync.errors` field.
	SyncError ErrorSource = "status.sync.errors"
)

// RepoSyncCondition describes the state of a RepoSync at a certain point.
type RepoSyncCondition struct {
	// type of RepoSync condition.
	Type RepoSyncConditionType `json:"type"`
	// status of the condition, one of True, False, Unknown.
	Status metav1.ConditionStatus `json:"status"`
	// The last time this condition was updated.
	// +nullable
	// +optional
	LastUpdateTime metav1.Time `json:"lastUpdateTime,omitempty"`
	// Last time the condition transitioned from one status to another.
	// +nullable
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
	// The reason for the condition's last transition.
	// +optional
	Reason string `json:"reason,omitempty"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty"`
	// hash of the source of truth. It can be a git commit hash, or an OCI image digest.
	// +optional
	Commit string `json:"commit,omitempty"`
	// errors is a list of errors that occurred in the process.
	// This field is used to track errors when the condition type is Reconciling or Stalled.
	// When the condition type is Syncing, the `errorSourceRefs` field is used instead to
	// avoid duplicating errors between `status.conditions` and `status.rendering|source|sync`.
	// +optional
	Errors []ConfigSyncError `json:"errors,omitempty"`
	// errorSourceRefs track the origination(s) of errors when the condition type is Syncing.
	// +optional
	ErrorSourceRefs []ErrorSource `json:"errorSourceRefs,omitempty"`
	// errorSummary summarizes the errors in the `errors` field when the condition type is Reconciling or Stalled,
	// and summarizes the errors referred in the `errorsSourceRefs` field when the condition type is Syncing.
	// +optional
	ErrorSummary *ErrorSummary `json:"errorSummary,omitempty"`
}

// +kubebuilder:object:root=true

// RepoSyncList contains a list of RepoSync
type RepoSyncList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RepoSync `json:"items"`
}
