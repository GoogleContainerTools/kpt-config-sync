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

package testing

const (
	// ArtifactRegistryHost is the host address of the Artifact Registry
	ArtifactRegistryHost = "us-docker.pkg.dev"
	// GoogleContainerRegistryHost is the host address of the Container Registry
	GoogleContainerRegistryHost = "gcr.io"
	// CSRHost is the host address of the Cloud Source Repositories
	CSRHost = "https://source.developers.google.com"

	// SSMInstanceID is the ID of the Secure Source Manager instance to be used by SSM tests
	SSMInstanceID = "configsync-test"

	// TestInfraContainerRepositoryPath is the Config Sync test-infra repository path
	TestInfraContainerRepositoryPath = "kpt-config-sync-ci-artifacts"
	// TestInfraArtifactRepositoryPath is the Config Sync test-infra repository path
	TestInfraArtifactRepositoryPath = "kpt-config-sync-ci-artifacts/test-infra"
	// ConfigSyncTestPublicRepositoryPath is the Config Sync config-sync-test-public repository path
	ConfigSyncTestPublicRepositoryPath = "kpt-config-sync-ci-artifacts/config-sync-test-public"

	// TestInfraContainerRepositoryAddress is the Config Sync test-infra repository hosted on GCR
	TestInfraContainerRepositoryAddress = GoogleContainerRegistryHost + "/" + TestInfraContainerRepositoryPath
	// TestInfraArtifactRepositoryAddress is the Config Sync test-infra repository hosted on GAR
	TestInfraArtifactRepositoryAddress = ArtifactRegistryHost + "/" + TestInfraArtifactRepositoryPath
	// ConfigSyncTestPublicRepositoryAddress is the Config Sync config-sync-test-public repository hosted on GAR
	ConfigSyncTestPublicRepositoryAddress = ArtifactRegistryHost + "/" + ConfigSyncTestPublicRepositoryPath

	// HTTPDImage is the httpd image used by on-cluster test components
	HTTPDImage = "httpd:2"
	// RegistryImage is the registry image used by on-cluster test components
	RegistryImage = "registry:2"
	// NginxImage is the nginx image used by on-cluster test components
	NginxImage = "nginx:1.23"
	// PrometheusImage is the prometheus image used by on-cluster test components
	PrometheusImage = "prom/prometheus:v2.37.6"
)
