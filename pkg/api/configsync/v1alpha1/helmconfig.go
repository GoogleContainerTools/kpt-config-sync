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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"kpt.dev/configsync/pkg/api/configsync"
)

// HelmRootSync contains the configuration specific to locate, download and template a Helm chart.
type HelmRootSync struct {
	HelmBase `json:",inline"`
	// namespace sets the target namespace for a release.
	// Default: "default".
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// deployNamespace specifies the namespace in which to deploy the chart.
	// This is a mutually exclusive setting with "namespace".
	// If neither namespace nor deployNamespace are set, the chart will be
	// deployed into the default namespace.
	// +optional
	DeployNamespace string `json:"deployNamespace,omitempty"`
}

// HelmRepoSync contains the configuration specific to locate, download and template a Helm chart.
type HelmRepoSync struct {
	HelmBase `json:",inline"`
}

// HelmBase contains the configuration specific to locate, download and template a Helm chart.
type HelmBase struct {
	// repo is the helm repository URL to sync from. Required.
	Repo string `json:"repo"`

	// chart is a Helm chart name. Required.
	Chart string `json:"chart"`

	// version is the chart version.
	// This can be specified as a static version, or as a range of values from which Config Sync
	// will fetch the latest. If left empty, Config Sync will fetch the latest version according to semver.
	// The supported version range syntax is identical to the version range syntax
	// supported by helm CLI, and is documented here: https://github.com/Masterminds/semver#hyphen-range-comparisons.
	// Versions specified as a range, the literal tag "latest", or left empty to indicate that Config Sync should
	// fetch the latest version, will be fetched every sync according to spec.helm.period.
	// +optional
	Version string `json:"version,omitempty"`

	// releaseName is the name of the Helm release.
	// +optional
	ReleaseName string `json:"releaseName,omitempty"`

	// values to use instead of default values that accompany the chart. Format
	// values the same as default values.yaml. If `valuesFileRefs` is also specified,
	// fields from `values` will override fields from `valuesFileRefs`.
	// +optional
	Values *apiextensionsv1.JSON `json:"values,omitempty"`

	// valuesFileRefs holds references to objects in the cluster that represent
	// values to use instead of default values that accompany the chart. Currently,
	// only ConfigMaps are supported. The ConfigMaps must be immutable and in the same
	// namespace as the RootSync/RepoSync. When multiple values files are specified, duplicated
	// keys in later files will override the value from earlier files. This is equivalent
	// to passing in multiple values files to Helm CLI. If `values` is also specified,
	// fields from `values` will override fields from `valuesFileRefs`.
	// +optional
	ValuesFileRefs []ValuesFileRef `json:"valuesFileRefs,omitempty"`

	// includeCRDs specifies if Helm template should also generate CustomResourceDefinitions.
	// If IncludeCRDs is set to false, no CustomeResourceDefinition will be generated.
	// Default: false.
	// +optional
	IncludeCRDs bool `json:"includeCRDs,omitempty"`

	// period is the time duration that Config Sync waits before refetching the chart.
	// Default: 1 hour.
	// Use string to specify this field value, like "30s", "5m".
	// More details about valid inputs: https://pkg.go.dev/time#ParseDuration.
	// If the chart version is a range, the literal tag "latest", or left empty to indicate that Config Sync
	// should fetch the latest version, the chart will be re-fetched according to spec.helm.period.
	// If the chart version is specified as a single static version, the chart will not be re-fetched.
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

	// caCertSecretRef specifies the name of the secret where the CA certificate is stored.
	// The creation of the secret should be done out of band by the user and should store the
	// certificate in a key named "cert". For RepoSync resources, the secret must be
	// created in the same namespace as the RepoSync. For RootSync resource, the secret
	// must be created in the config-management-system namespace.
	// +nullable
	// +optional
	CACertSecretRef *SecretReference `json:"caCertSecretRef,omitempty"`
}

// ValuesFileRef references a ConfigMap object that contains a values file to use for
// helm rendering. The ConfigMap must be in the same namespace as the RootSync/RepoSync.
type ValuesFileRef struct {
	// name represents the Object name. Required.
	Name string `json:"name,omitempty"`

	// dataKey represents the object data key to read the values from. Default: `values.yaml`
	// +optional
	DataKey string `json:"dataKey,omitempty"`
}
