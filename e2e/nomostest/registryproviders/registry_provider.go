// Copyright 2024 Google LLC
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

package registryproviders

import (
	"fmt"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/testshell"
)

// RegistryProvider is an interface for the remote Git providers.
type RegistryProvider interface {
	Type() string

	// PushURL returns the push URL for the registry.
	// It is used when pushing images to the remote registry from the test execution.
	// For the testing registry-server, RemoteURL uses localhost and forwarded port, while SyncURL uses the DNS.
	// For other git providers, RemoteURL should be the same as SyncURL.
	// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
	PushURL(name string) (string, error)

	// SyncURL returns the registry URL for Config Sync to sync from using OCI.
	// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
	SyncURL(name string) string
	// Setup is used to perform initialization of the RegistryProvider before the
	// tests begin.
	Setup() error
	// Teardown is used to perform cleanup of the RegistryProvider after test
	// completion.
	Teardown() error
	// Login to the registry with the client
	Login() error
	// Logout of the registry with the client
	Logout() error
	// deleteImage is used to delegate image deletion to the RegistryProvider
	// implementation. This is needed because the delete interface varies by
	// provider.
	deleteImage(name, digest string) error
}

// NewOCIProvider creates a RegistryProvider for the specific OCI provider type.
// This enables writing tests that can run against multiple types of registries.
func NewOCIProvider(provider, repoName string, shell *testshell.TestShell) RegistryProvider {
	repositoryName := fmt.Sprintf("config-sync-e2e-test--%s", repoName)
	repositorySuffix := "oci"
	switch provider {
	case e2e.ArtifactRegistry:
		return &ArtifactRegistryOCIProvider{
			ArtifactRegistryProvider{
				project:  *e2e.GCPProject,
				location: DefaultLocation,
				// Use cluster name to avoid overlap.
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
				shell:            shell,
			},
		}
	default:
		return &LocalOCIProvider{
			LocalProvider{
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
				shell:            shell,
			},
		}
	}
}

// NewHelmProvider creates a RegistryProvider for the specific helm provider type.
// This enables writing tests that can run against multiple types of registries.
func NewHelmProvider(provider, repoName string, shell *testshell.TestShell) RegistryProvider {
	repositoryName := fmt.Sprintf("config-sync-e2e-test--%s", repoName)
	repositorySuffix := "helm"
	switch provider {
	case e2e.ArtifactRegistry:
		return &ArtifactRegistryHelmProvider{
			ArtifactRegistryProvider{
				project:  *e2e.GCPProject,
				location: DefaultLocation,
				// Use cluster name to avoid overlap.
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
				shell:            shell,
			},
		}
	default:
		return &LocalHelmProvider{
			LocalProvider{
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
				shell:            shell,
			},
		}
	}
}
