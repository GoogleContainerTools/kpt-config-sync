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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Oci contains configuration specific to importing resources from an OCI package.
type Oci struct {
	// image is the OCI image repository URL for the package to sync from.
	// e.g. `LOCATION-docker.pkg.dev/PROJECT_ID/REPOSITORY_NAME/PACKAGE_NAME`.
	// The image can be pulled by TAG or by DIGEST if it is specified in PACKAGE_NAME.
	// - Pull by tag: `LOCATION-docker.pkg.dev/PROJECT_ID/REPOSITORY_NAME/PACKAGE_NAME:TAG`.
	// - Pull by digest: `LOCATION-docker.pkg.dev/PROJECT_ID/REPOSITORY_NAME/PACKAGE_NAME@sha256:DIGEST`.
	// If neither TAG nor DIGEST is specified, it pulls with the `latest` tag by default.
	// Required
	Image string `json:"image"`

	// dir is the absolute path of the directory that contains
	// the local resources.  Default: the root directory of the image.
	// +optional
	Dir string `json:"dir,omitempty"`

	// period is the time duration between consecutive syncs. Default: 15s.
	// Note to developers that customers specify this value using
	// string (https://golang.org/pkg/time/#Duration.String) like "3s"
	// in their Custom Resource YAML. However, time.Duration is at a nanosecond
	// granularity, and it is easy to introduce a bug where it looks like the
	// code is dealing with seconds but its actually nanoseconds (or vice versa).
	// +optional
	Period metav1.Duration `json:"period,omitempty"`

	// auth is the type of secret configured for access to the OCI package.
	// Must be one of gcenode, gcpserviceaccount, or none.
	// The validation of this is case-sensitive. Required.
	//
	// +kubebuilder:validation:Pattern=^(gcenode|gcpserviceaccount|none)$
	Auth string `json:"auth"`

	// gcpServiceAccountEmail specifies the GCP service account used to annotate
	// the RootSync/RepoSync controller Kubernetes Service Account.
	// Note: The field is used when secretType: gcpServiceAccount.
	GCPServiceAccountEmail string `json:"gcpServiceAccountEmail,omitempty"`
}
