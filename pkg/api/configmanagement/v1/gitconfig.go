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

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Git contains the configs needed by GitPolicyImporter.
type Git struct {
	// Repo is the git repository URL to sync from. Required.
	Repo string `json:"repo"`

	// Branch is the git branch to checkout. Default: "master".
	// +optional
	Branch string `json:"branch,omitempty"`

	// Revision is the git revision (tag, ref or commit) to fetch.
	// +optional
	Revision string `json:"revision,omitempty"`

	// Dir is the absolute path of the directory that contains
	// the local policy.  Default: the root directory of the repo.
	// +optional
	Dir string `json:"dir,omitempty"`

	// Period is the time duration between consecutive syncs. Default: 15s.
	// Note to developers that customers specify this value using
	// string (https://golang.org/pkg/time/#Duration.String) like "3s"
	// in their Custom Resource YAML. However, time.Duration is at a nanosecond
	// granularity, and it's easy to introduce a bug where it looks like the
	// code is dealing with seconds but its actually nanoseconds (or vice versa).
	// +optional
	Period metav1.Duration `json:"period,omitempty"`

	// Auth is the type of secret configured for access to the Git repo.
	// Must be one of ssh, cookiefile, gcenode, token, or none. Required.
	// The validation of this is case-sensitive. Required.
	//
	// +kubebuilder:validation:Pattern=^(ssh|cookiefile|gcenode|token|none)$
	Auth string `json:"auth"`

	// Proxy is a struct that contains options for configuring access to the Git repo via a proxy.
	// Only has an effect when secretType is one of ("cookiefile", "none"). Optional.
	// +optional
	Proxy string `json:"proxy,omitempty"`

	// SecretRef is the secret used to connect to the Git source of truth.
	// +optional
	SecretRef SecretReference `json:"secretRef,omitempty"`
}

// SecretReference contains the reference to the secret used to connect to
// Git source of truth.
type SecretReference struct {
	// Name represents the secret name.
	// +optional
	Name string `json:"name,omitempty"`
}
