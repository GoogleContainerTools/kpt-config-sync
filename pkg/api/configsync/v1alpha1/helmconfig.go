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
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"kpt.dev/configsync/pkg/api/configsync"
)

// Helm contains the configuration specific to locate, download and template a Helm chart.
type Helm struct {
	// repo is the helm repository URL to sync from. Required.
	Repo string `json:"repo"`

	// chart is a Helm chart name. Required.
	Chart string `json:"chart"`

	// version is the chart version. If this is not specified, the latest version is used
	// +optional
	Version string `json:"version,omitempty"`

	// releaseName is the name of the Helm release.
	// +optional
	ReleaseName string `json:"releaseName,omitempty"`

	// namespace sets the target namespace for a release
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// values to use instead of default values that accompany the chart
	// +optional
	Values unstructured.Unstructured `json:"values,omitempty"`

	// valuesFiles is a list of path to Helm value files.
	// Values files must be in the same repository with the Helm chart.
	// And the paths here are absolute path from the root directory of the repository
	// +optional
	ValuesFiles []string `json:"valuesFiles,omitempty"`

	// includeCRDs specifies if Helm template should also generate CustomResourceDefinitions.
	// If IncludeCRDs is set to false, no CustomeResourceDefinition will be generated.
	// Default: false.
	// +optional
	IncludeCRDs bool `json:"includeCRDs,omitempty"`

	// period is the time duration between consecutive syncs. Default: 15s.
	// Use string to specify this field value, like "30s", "5m".
	// More details about valid inputs: https://pkg.go.dev/time#ParseDuration.
	// Chart will not be re-synced if version is specified and it is not "latest"
	// +optional
	Period metav1.Duration `json:"period,omitempty"`

	// auth specifies the type to authenticate to the Helm repository.
	// Must be one of secret, gcpserviceaccount, or none.
	// The validation of this is case-sensitive. Required.
	// +kubebuilder:validation:Enum=none;gcpserviceaccount;token
	Auth configsync.AuthType `json:"auth"`

	// gcpServiceAccountEmail specifies the GCP service account used to annotate
	// the RootSync/RepoSync controller Kubernetes Service Account.
	// Note: The field is used when spec.helm.auth: gcpserviceaccount.
	// +optional
	GCPServiceAccountEmail string `json:"gcpServiceAccountEmail,omitempty"`

	// secretRef holds the authentication secret for accessing
	// the Helm repository.
	// +optional
	SecretRef SecretReference `json:"secretRef,omitempty"`
}
