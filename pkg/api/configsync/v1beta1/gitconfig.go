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

// Git contains the configs which specify how to connect to and read from a Git
// repository.
type Git struct {
	// repo is the git repository URL to sync from. Required.
	Repo string `json:"repo"`

	// branch is the git branch to checkout. Default: "master".
	// +optional
	Branch string `json:"branch,omitempty"`

	// revision is the git revision (tag, ref or commit) to fetch. Default: "HEAD".
	// +optional
	Revision string `json:"revision,omitempty"`

	// dir is the absolute path of the directory that contains
	// the local resources.  Default: the root directory of the repo.
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

	// auth is the type of secret configured for access to the Git repo.
	// Must be one of ssh, cookiefile, gcenode, token, or none.
	// The validation of this is case-sensitive. Required.
	//
	// +kubebuilder:validation:Pattern=^(ssh|cookiefile|gcenode|gcpserviceaccount|token|none)$
	Auth string `json:"auth"`

	// gcpServiceAccountEmail specifies the GCP service account used to annotate
	// the RootSync/RepoSync controller Kubernetes Service Account.
	// Note: The field is used when secretType: gcpServiceAccount.
	GCPServiceAccountEmail string `json:"gcpServiceAccountEmail,omitempty"`

	// proxy specifies an HTTPS proxy for accessing the Git repo.
	// Only has an effect when secretType is one of ("cookiefile", "none", "token").
	// When secretType is "cookiefile" or "token", if your HTTPS proxy URL contains sensitive information
	// such as a username or password and you need to hide the sensitive information,
	// you can leave this field empty and add the URL for the HTTPS proxy into the same Secret
	// used for the Git credential via `kubectl create secret ... --from-literal=https_proxy=HTTPS_PROXY_URL`. Optional.
	// +optional
	Proxy string `json:"proxy,omitempty"`

	// secretRef is the secret used to connect to the Git source of truth.
	// +optional
	SecretRef SecretReference `json:"secretRef,omitempty"`

	// noSSLVerify specifies whether to enable or disable the SSL certificate verification. Default: false.
	// If noSSLVerify is set to true, it tells Git to skip the SSL certificate verification.
	// +optional
	NoSSLVerify bool `json:"noSSLVerify,omitempty"`
}

// SecretReference contains the reference to the secret used to connect to
// Git source of truth.
type SecretReference struct {
	// name represents the secret name.
	// +optional
	Name string `json:"name,omitempty"`
}
