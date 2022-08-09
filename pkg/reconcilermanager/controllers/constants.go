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

const (
	// GCPSAAnnotationKey is used to annotate RepoSync/RootSync controller SA when
	// spec.git.auth: gcpserviceaccount is used with Workload Identity enabled on a
	// GKE cluster.
	// https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity
	GCPSAAnnotationKey = "iam.gke.io/gcp-service-account"
)

// Git secret configmap key names
const (
	// GitSecretConfigKeySSH is the key at which an ssh cert is stored
	GitSecretConfigKeySSH = "ssh"
	// GitSecretConfigKeyCookieFile is the key at which the git cookiefile is stored
	GitSecretConfigKeyCookieFile = "cookie_file"
	// GitSecretConfigKeyToken is the key at which a token's value is stored
	GitSecretConfigKeyToken = "token"
	// GitSecretConfigKeyTokenUsername is the key at which a token's username is stored
	GitSecretConfigKeyTokenUsername = "username"
)

// Helm secret data key names
const (
	// HelmSecretKeyToken is the key at which a token's value is stored
	HelmSecretKeyPassword = "password"
	// HelmSecretKeyUsername is the key at which a token's username is stored
	HelmSecretKeyUsername = "username"
)
