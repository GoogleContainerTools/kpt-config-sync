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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Spec provides a common type that is embedded in RepoSyncSpec and RootSyncSpec.
type Spec struct {
	// sourceFormat specifies how the repository is formatted.
	// See documentation for specifics of what these options do.
	//
	// Must be one of hierarchy, unstructured. Optional. Set to
	// hierarchy if not specified.
	//
	// The validation of this is case-sensitive.
	// +kubebuilder:validation:Pattern=^(hierarchy|unstructured|)$
	// +optional
	SourceFormat string `json:"sourceFormat,omitempty"`

	// sourceType specifies the type of the source of truth.
	//
	// Must be one of git, oci, helm. Optional. Set to git if not specified.
	// +kubebuilder:validation:Pattern=^(git|oci|helm)$
	// +kubebuilder:default:=git
	// +optional
	SourceType string `json:"sourceType,omitempty"`

	// git contains configuration specific to importing resources from a Git repo.
	// +optional
	*Git `json:"git,omitempty"`

	// oci contains configuration specific to importing resources from an OCI package.
	// +optional
	Oci *Oci `json:"oci,omitempty"`

	// helm contains configuration specific to importing resources from a Helm repo.
	// +optional
	Helm *Helm `json:"helm,omitempty"`

	// override allows to override the settings for a reconciler.
	// +nullable
	// +optional
	Override OverrideSpec `json:"override,omitempty"`
}

// Status provides a common type that is embedded in RepoSyncStatus and RootSyncStatus.
type Status struct {
	// observedGeneration is the most recent generation observed for the sync resource.
	// It corresponds to the it's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// reconciler is the name of the reconciler process which corresponds to the
	// sync resource.
	// +optional
	Reconciler string `json:"reconciler,omitempty"`

	// lastSyncedCommit describes the most recent hash that is successfully synced.
	// It can be a git commit hash, or an OCI image digest.
	// +optional
	LastSyncedCommit string `json:"lastSyncedCommit,omitempty"`

	// source contains fields describing the status of a *Sync's source of
	// truth.
	// +optional
	Source SourceStatus `json:"source,omitempty"`

	// rendering contains fields describing the status of rendering resources from
	// the source of truth.
	// +optional
	Rendering RenderingStatus `json:"rendering,omitempty"`

	// sync contains fields describing the status of syncing resources from the
	// source of truth to the cluster.
	// +optional
	Sync SyncStatus `json:"sync,omitempty"`
}

// SourceStatus describes the source status of a source-of-truth.
type SourceStatus struct {
	// gitStatus contains fields describing the status of a Git source of truth.
	// +optional
	Git *GitStatus `json:"gitStatus,omitempty"`

	// ociStatus contains fields describing the status of an OCI source of truth.
	// +optional
	Oci *OciStatus `json:"ociStatus,omitempty"`

	// hash of the source of truth that is rendered.
	// It can be a git commit hash, or an OCI image digest.
	// +optional
	Commit string `json:"commit,omitempty"`

	// lastUpdate is the timestamp of when this status was last updated by a
	// reconciler.
	// +nullable
	// +optional
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`

	// errors is a list of any errors that occurred while reading from the source of truth.
	// +optional
	Errors []ConfigSyncError `json:"errors,omitempty"`

	// errorSummary summarizes the errors encountered during the process of reading from the source of truth.
	// +optional
	ErrorSummary *ErrorSummary `json:"errorSummary,omitempty"`
}

// RenderingStatus describes the status of rendering the source DRY configs to the WET format.
type RenderingStatus struct {
	// gitStatus contains fields describing the status of a Git source of truth.
	// +optional
	Git *GitStatus `json:"gitStatus,omitempty"`

	// ociStatus contains fields describing the status of an OCI source of truth.
	// +optional
	Oci *OciStatus `json:"ociStatus,omitempty"`

	// hash of the source of truth that is rendered.
	// It can be a git commit hash, or an OCI image digest.
	// +optional
	Commit string `json:"commit,omitempty"`

	// Human-readable message describes details about the rendering status.
	Message string `json:"message,omitempty"`

	// lastUpdate is the timestamp of when this status was last updated by a
	// reconciler.
	// +nullable
	// +optional
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`

	// errors is a list of any errors that occurred while rendering the source of truth.
	// +optional
	Errors []ConfigSyncError `json:"errors,omitempty"`

	// errorSummary summarizes the errors encountered during the process of rendering the source of truth.
	// +optional
	ErrorSummary *ErrorSummary `json:"errorSummary,omitempty"`
}

// SyncStatus provides the status of the syncing of resources from a source-of-truth on to the cluster.
type SyncStatus struct {
	// gitStatus contains fields describing the status of a Git source of truth.
	// +optional
	Git *GitStatus `json:"gitStatus,omitempty"`

	// ociStatus contains fields describing the status of an OCI source of truth.
	// +optional
	Oci *OciStatus `json:"ociStatus,omitempty"`

	// hash of the source of truth that is rendered.
	// It can be a git commit hash, or an OCI image digest.
	// +optional
	Commit string `json:"commit,omitempty"`

	// lastUpdate is the timestamp of when this status was last updated by a
	// reconciler.
	// +nullable
	// +optional
	LastUpdate metav1.Time `json:"lastUpdate,omitempty"`

	// errors is a list of any errors that occurred while applying the resources
	// from the change indicated by Commit.
	// +optional
	Errors []ConfigSyncError `json:"errors,omitempty"`

	// errorSummary summarizes the errors encountered during the process of syncing the resources.
	// +optional
	ErrorSummary *ErrorSummary `json:"errorSummary,omitempty"`
}

// GitStatus describes the status of a Git source of truth.
type GitStatus struct {
	// repo is the git repository URL being synced from.
	Repo string `json:"repo"`

	// revision is the git revision (tag, ref, or commit) being fetched.
	Revision string `json:"revision"`

	// branch is the git branch being fetched
	Branch string `json:"branch"`

	// dir is the path within the Git repository that represents the top level of the repo to sync.
	// Default: the root directory of the repository
	Dir string `json:"dir"`
}

// OciStatus describes the status of the source of truth of an OCI image.
type OciStatus struct {
	// image is the OCI image repository URL for the package to sync from.
	Image string `json:"image"`

	// dir is the absolute path of the directory that contains the local resources.
	// Default: the root directory of the repository
	Dir string `json:"dir"`
}

// ConfigSyncError represents an error that occurs while parsing, applying, or
// remediating a resource.
type ConfigSyncError struct {
	// code is the error code of this particular error.  Error codes are numeric strings,
	// like "1012".
	Code string `json:"code"`

	// errorMessage describes the error that occurred.
	ErrorMessage string `json:"errorMessage"`

	// errorResources describes the resources associated with this error, if any.
	// +optional
	Resources []ResourceRef `json:"errorResources,omitempty"`
}

// ErrorSummary summarizes the errors encountered.
type ErrorSummary struct {
	// totalCount tracks the total number of errors.
	TotalCount int `json:"totalCount,omitempty"`
	// truncated indicates whether the `Errors` field includes all the errors.
	// If `true`, the `Errors` field does not includes all the errors.
	// If `false`, the `Errors` field includes all the errors.
	// The size limit of a RootSync/RepoSync object is 2MiB. The status update would
	// fail with the `ResourceExhausted` rpc error if there are too many errors.
	Truncated bool `json:"truncated,omitempty"`
	// errorCountAfterTruncation tracks the number of errors in the `Errors` field.
	ErrorCountAfterTruncation int `json:"errorCountAfterTruncation,omitempty"`
}

// ResourceRef contains the identification bits of a single managed resource.
type ResourceRef struct {
	// sourcePath is the repo-relative slash path to where the config is defined.
	// This field may be empty for errors that are not associated with a specific
	// config file.
	// +optional
	SourcePath string `json:"sourcePath,omitempty"`

	// name is the name of the affected K8S resource. This field may be empty for
	// errors that are not associated with a specific resource.
	// +optional
	Name string `json:"name,omitempty"`

	// namespace is the namespace of the affected K8S resource. This field may be
	// empty for errors that are associated with a cluster-scoped resource or not
	// associated with a specific resource.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// gvk is the GroupVersionKind of the affected K8S resource. This field may be
	// empty for errors that are not associated with a specific resource.
	// +optional
	GVK metav1.GroupVersionKind `json:"gvk,omitempty"`
}

// SourceType specifies the type of the source of truth.
type SourceType string

const (
	// GitSource represents the source type is Git repository.
	GitSource SourceType = "git"

	// OciSource represents the source type is OCI package.
	OciSource SourceType = "oci"

	// HelmSource represents the source type is Helm repository.
	HelmSource SourceType = "helm"
)
