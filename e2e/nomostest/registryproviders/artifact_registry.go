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
	"strings"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/testshell"
)

// DefaultLocation is the default location in which to host Artifact Registry
// repositories. In this case, `us`, which is a multi-region location.
const DefaultLocation = "us"

// ArtifactRegistryReaderName is the name of the google service account
// with permission to read from Artifact Registry.
const ArtifactRegistryReaderName = "e2e-test-ar-reader"

// ArtifactRegistryReaderEmail returns the email of the google service account
// with permission to read from Artifact Registry.
func ArtifactRegistryReaderEmail() string {
	return fmt.Sprintf("%s@%s.iam.gserviceaccount.com",
		ArtifactRegistryReaderName, *e2e.GCPProject)
}

// ArtifactRegistryProvider is the provider type for Google Artifact Registry
type ArtifactRegistryProvider struct {
	// project in which to store the image
	project string
	// location to store the image
	location string
	// repositoryName in which to store the image
	repositoryName string
	// repositorySuffix is a suffix appended after the repository name. This enables
	// separating different artifact types within the repository
	// (e.g. helm chart vs basic oci image). This is not strictly necessary but
	// provides a guardrail to help prevent gotchas when mixing usage of helm-sync
	// and oci-sync.
	repositorySuffix string
	// shell used for invoking CLI tools
	shell *testshell.TestShell
}

// Type returns the provider type.
func (a *ArtifactRegistryProvider) Type() string {
	return e2e.ArtifactRegistry
}

// Teardown preforms teardown
// Does nothing currently. We may want to have this delete the repository to
// minimize side effects.
func (a *ArtifactRegistryProvider) Teardown() error {
	return nil
}

// registryHost returns the domain of the artifact registry
func (a *ArtifactRegistryProvider) registryHost() string {
	return fmt.Sprintf("%s-docker.pkg.dev", a.location)
}

// repositoryAddress returns the domain and path to the chart repository
func (a *ArtifactRegistryProvider) repositoryAddress() string {
	return fmt.Sprintf("%s/%s/%s/%s", a.registryHost(), a.project, a.repositoryName, a.repositorySuffix)
}

// createRepository uses gcloud to create the repository, if it doesn't exist.
func (a *ArtifactRegistryProvider) createRepository() error {
	out, err := a.shell.ExecWithDebug("gcloud", "artifacts", "repositories",
		"describe", a.repositoryName,
		"--location", a.location,
		"--project", a.project)
	if err != nil {
		if !strings.Contains(string(out), "NOT_FOUND") {
			return fmt.Errorf("failed to describe image repository: %w", err)
		}
		// repository does not exist, continue with creation
	} else {
		// repository already exists, skip creation
		return nil
	}

	_, err = a.shell.ExecWithDebug("gcloud", "artifacts", "repositories",
		"create", a.repositoryName,
		"--repository-format", "docker",
		"--location", a.location,
		"--project", a.project)
	if err != nil {
		return fmt.Errorf("failed to create image repository: %w", err)
	}
	return nil
}

// deleteImage the package from the remote registry, including all versions and tags.
func (a *ArtifactRegistryProvider) deleteImage(name, digest string) error {
	imageURL := fmt.Sprintf("%s/%s@%s", a.repositoryAddress(), name, digest)
	if _, err := a.shell.ExecWithDebug("gcloud", "artifacts", "docker", "images", "delete", imageURL, "--delete-tags", "--project", a.project); err != nil {
		return fmt.Errorf("deleting image from registry: %w", err)
	}
	return nil
}

// ArtifactRegistryOCIProvider provides methods for interacting with the test registry-server
// using the oci-sync interface.
type ArtifactRegistryOCIProvider struct {
	ArtifactRegistryProvider
}

// Login to the registry with the crane client
func (a *ArtifactRegistryOCIProvider) Login() error {
	var err error
	authCmd := a.shell.Command("gcloud", "auth", "print-access-token")
	loginCmd := a.shell.Command("crane", "auth", "login",
		"-u", "oauth2accesstoken", "--password-stdin",
		a.registryHost())
	loginCmd.Stdin, err = authCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating STDOUT pipe: %w", err)
	}
	if err := loginCmd.Start(); err != nil {
		return fmt.Errorf("starting login command: %w", err)
	}
	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("running print-access-token command: %w", err)
	}
	if err := loginCmd.Wait(); err != nil {
		return fmt.Errorf("waiting for login command: %w", err)
	}
	return nil
}

// Logout of the registry with the crane client
func (a *ArtifactRegistryOCIProvider) Logout() error {
	_, err := a.shell.ExecWithDebug("crane", "auth", "logout", a.registryHost())
	if err != nil {
		return fmt.Errorf("running logout command: %w", err)
	}
	return nil
}

// Setup performs setup
func (a *ArtifactRegistryOCIProvider) Setup() error {
	return a.createRepository()
}

// PushURL returns a URL for pushing images to the remote registry.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (a *ArtifactRegistryOCIProvider) PushURL(name string) (string, error) {
	return fmt.Sprintf("%s/%s", a.repositoryAddress(), name), nil
}

// SyncURL returns a URL for Config Sync to sync from using OCI.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (a *ArtifactRegistryOCIProvider) SyncURL(name string) string {
	return fmt.Sprintf("%s/%s", a.repositoryAddress(), name)
}

// ArtifactRegistryHelmProvider provides methods for interacting with the test registry-server
// using the helm-sync interface.
type ArtifactRegistryHelmProvider struct {
	ArtifactRegistryProvider
}

// Login to the registry with the helm client
func (a *ArtifactRegistryHelmProvider) Login() error {
	var err error
	authCmd := a.shell.Command("gcloud", "auth", "print-access-token")
	loginCmd := a.shell.Command("helm", "registry", "login",
		"-u", "oauth2accesstoken", "--password-stdin",
		a.registryHost())
	loginCmd.Stdin, err = authCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating STDOUT pipe: %w", err)
	}
	if err := loginCmd.Start(); err != nil {
		return fmt.Errorf("starting login command: %w", err)
	}
	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("running print-access-token command: %w", err)
	}
	if err := loginCmd.Wait(); err != nil {
		return fmt.Errorf("waiting for login command: %w", err)
	}
	return nil
}

// Logout of the registry with the helm client
func (a *ArtifactRegistryHelmProvider) Logout() error {
	_, err := a.shell.ExecWithDebug("helm", "registry", "logout", a.registryHost())
	if err != nil {
		return fmt.Errorf("running logout: %w", err)
	}
	return nil
}

// Setup performs required setup for the helm AR provider.
// This requires setting up authentication for the helm CLI.
func (a *ArtifactRegistryHelmProvider) Setup() error {
	return a.createRepository()
}

// PushURL returns a URL for pushing images to the remote registry.
// The name parameter is ignored because helm CLI appends the chart name to the image.
func (a *ArtifactRegistryHelmProvider) PushURL(_ string) (string, error) {
	return a.SyncURL(""), nil
}

// SyncURL returns a URL for Config Sync to sync from using OCI.
// The name parameter is ignored because helm CLI appends the chart name to the image.
func (a *ArtifactRegistryHelmProvider) SyncURL(_ string) string {
	return fmt.Sprintf("oci://%s", a.repositoryAddress())
}
