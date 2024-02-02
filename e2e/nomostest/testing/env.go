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
	// ARHost is the host address of the Artifact Registry
	ARHost = "us-docker.pkg.dev"

	// GCRHost is the host address of the Container Registry
	GCRHost = "gcr.io"

	// CSRHost is the host address of the Cloud Source Repositories
	CSRHost = "https://source.developers.google.com"

	// TestInfraContainerRegistry is the Config Sync test-infra registry hosted on GCR
	TestInfraContainerRegistry = GCRHost + "/kpt-config-sync-ci-artifacts"
	// TestInfraArtifactRegistry is the Config Sync test-infra registry hosted on GAR
	TestInfraArtifactRegistry = ARHost + "/kpt-config-sync-ci-artifacts/test-infra"
	// ConfigSyncTestPublicRegistry is the Config Sync config-sync-test-public registry hosted on GAR
	ConfigSyncTestPublicRegistry = ARHost + "/kpt-config-sync-ci-artifacts/config-sync-test-public"
)
