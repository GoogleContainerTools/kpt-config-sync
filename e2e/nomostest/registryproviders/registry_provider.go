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
}

// NewOCIProvider creates a RegistryProvider for the specific OCI provider type.
// This enables writing tests that can run against multiple types of registries.
func NewOCIProvider(provider string) RegistryProvider {
	switch provider {
	// TODO: Refactor existing Artifact Registry impl to this interface
	default:
		return &LocalOCIProvider{}
	}
}
