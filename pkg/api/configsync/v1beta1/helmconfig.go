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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
)

// HelmRootSync contains the configuration specific to locate, download and template a Helm chart for RootSync.
type HelmRootSync struct {
	HelmBase `json:",inline"`
	// namespace sets the value of {{Release.Namespace}} defined in the chart templates.
	// This is a mutually exclusive setting with "deployNamespace".
	// Default: default.
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// deployNamespace specifies the namespace in which to deploy the chart.
	// This is a mutually exclusive setting with "namespace".
	// If neither namespace nor deployNamespace are set, the chart will be
	// deployed into the default namespace.
	// +optional
	DeployNamespace string `json:"deployNamespace,omitempty"`
}

// HelmRepoSync contains the configuration specific to locate, download and template a Helm chart for RepoSync.
type HelmRepoSync struct {
	HelmBase `json:",inline"`
}

// HelmBase contains the configuration specific to locate, download and template a Helm chart.
type HelmBase struct {
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

	// values to use instead of default values that accompany the chart. Format
	// values the same as default values.yaml. These values will take precedence if
	// used in conjunction with valuesFrom.
	// +optional
	Values *apiextensionsv1.JSON `json:"values,omitempty"`

	// valuesFrom holds references to objects in the cluster that represent
	// values to use instead of default values that accompany the chart. Currently,
	// only ConfigMaps are supported. Objects listed later will take precedence.
	// +optional
	ValuesFrom []ValuesFrom `json:"valuesFrom,omitempty"`

	// valuesMergeMode specifies the strategy for handling multiple valueFiles.
	// Set to 'override' to have each valuesFile sent to helm CLI individually.
	// Set to 'merge' to have valuesFiles merged together into one valuesFile.
	// +kubebuilder:validation:Enum=override;merge
	// +kubebuilder:default:=override
	// +optional
	ValuesMergeMode string `json:"valuesMergeMode,omitempty"`

	// includeCRDs specifies if Helm template should also generate CustomResourceDefinitions.
	// If IncludeCRDs is set to false, no CustomeResourceDefinition will be generated.
	// Default: false.
	// +optional
	IncludeCRDs bool `json:"includeCRDs,omitempty"`

	// period is the time duration between consecutive syncs. Default: 15s.
	// Use string to specify this field value, like "30s", "5m".
	// More details about valid inputs: https://pkg.go.dev/time#ParseDuration.
	// Chart will not be resynced if version is specified.
	// Note: Resyncing chart for "latest" version is not supported in feature preview.
	// +optional
	Period metav1.Duration `json:"period,omitempty"`

	// auth specifies the type to authenticate to the Helm repository.
	// Must be one of token, gcpserviceaccount, gcenode or none.
	// The validation of this is case-sensitive. Required.
	// +kubebuilder:validation:Enum=none;gcpserviceaccount;token;gcenode
	Auth configsync.AuthType `json:"auth"`

	// gcpServiceAccountEmail specifies the GCP service account used to annotate
	// the RootSync/RepoSync controller Kubernetes Service Account.
	// Note: The field is used when spec.helm.auth: gcpserviceaccount.
	// +optional
	GCPServiceAccountEmail string `json:"gcpServiceAccountEmail,omitempty"`

	// secretRef holds the authentication secret for accessing
	// the Helm repository.
	// +nullable
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

// ValuesFrom holds references to objects in the cluster that represent
// values to use instead of default values that accompany the chart.
type ValuesFrom struct {
	// kind represents the Object kind. Must be `ConfigMap`. Required.
	// +kubebuilder:validation:Enum=ConfigMap
	Kind string `json:"kind,omitempty"`

	// name represents the Object name. Required.
	Name string `json:"name,omitempty"`

	// key represents the object data key to read the value from.
	// +kubebuilder:default:=values.yaml
	// +optional
	Key string `json:"key,omitempty"`
}
