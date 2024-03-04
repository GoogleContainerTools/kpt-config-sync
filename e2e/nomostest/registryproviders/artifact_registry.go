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
	// gcloudClient used for executing gcloud commands
	gcloudClient GcloudClient
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

// repositoryLocalAddress returns the address of the repository in the
// registry for use by local clients, like the helm or crane clients.
func (a *ArtifactRegistryProvider) repositoryLocalAddress() (string, error) {
	return a.repositoryRemoteAddress()
}

// repositoryRemoteAddress returns the address of the repository in the
// registry for use by remote clients, like Kubernetes or Config Sync.
func (a *ArtifactRegistryProvider) repositoryRemoteAddress() (string, error) {
	return fmt.Sprintf("%s/%s/%s/%s", a.registryHost(), a.project, a.repositoryName, a.repositorySuffix), nil
}

// ImageLocalAddress returns the address of the image in the registry for
// use by local clients, like the helm or crane clients.
func (a *ArtifactRegistryProvider) ImageLocalAddress(imageName string) (string, error) {
	repoAddr, err := a.repositoryLocalAddress()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", repoAddr, imageName), nil
}

// ImageRemoteAddress returns the address of the image in the registry for
// use by remote clients, like Kubernetes or Config Sync.
func (a *ArtifactRegistryProvider) ImageRemoteAddress(imageName string) (string, error) {
	repoAddr, err := a.repositoryRemoteAddress()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", repoAddr, imageName), nil
}

// createRepository uses gcloud to create the repository, if it doesn't exist.
func (a *ArtifactRegistryProvider) createRepository() error {
	out, err := a.gcloudClient.Gcloud("artifacts", "repositories",
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

	_, err = a.gcloudClient.Gcloud("artifacts", "repositories",
		"create", a.repositoryName,
		"--repository-format", "docker",
		"--location", a.location,
		"--project", a.project)
	if err != nil {
		return fmt.Errorf("failed to create image repository: %w", err)
	}
	return nil
}

// deleteImage deletes the OCI image from Artifact Registry, including all
// versions and tags.
//
// Helm doesn't provide a way to delete remote package images, so we're using
// gcloud instead. For helm charts, the image name is the chart name.
func (a *ArtifactRegistryProvider) deleteImage(imageName, digest string) error {
	imageAddress, err := a.ImageRemoteAddress(imageName)
	if err != nil {
		return err
	}
	imageAddress = fmt.Sprintf("%s@%s", imageAddress, digest)
	if _, err := a.gcloudClient.Gcloud("artifacts", "docker", "images", "delete", imageAddress, "--delete-tags", "--project", a.project); err != nil {
		return fmt.Errorf("deleting image from registry %s: %w", imageAddress, err)
	}
	return nil
}

// ArtifactRegistryOCIProvider provides methods for interacting with the test registry-server
// using the oci-sync interface.
type ArtifactRegistryOCIProvider struct {
	ArtifactRegistryProvider

	OCIClient *testshell.OCIClient
}

// Client for executing crane commands.
func (a *ArtifactRegistryOCIProvider) Client() CraneClient {
	return a.OCIClient
}

// Login to the registry with the crane client
func (a *ArtifactRegistryOCIProvider) Login() error {
	registryHost := a.registryHost()
	if err := a.OCIClient.Login(registryHost); err != nil {
		return fmt.Errorf("OCIClient.Login(%q): %w", registryHost, err)
	}
	return nil
}

// Logout of the registry with the crane client
func (a *ArtifactRegistryOCIProvider) Logout() error {
	registryHost := a.registryHost()
	if err := a.OCIClient.Logout(registryHost); err != nil {
		return fmt.Errorf("OCIClient.Logout(%q): %w", registryHost, err)
	}
	return nil
}

// Setup performs setup
func (a *ArtifactRegistryOCIProvider) Setup() error {
	return a.createRepository()
}

// PushImage pushes the local tarball as an OCI image to the remote registry.
func (a *ArtifactRegistryOCIProvider) PushImage(imageName, tag, localSourceTgzPath string) (*OCIImage, error) {
	imageLocalAddress, err := a.ImageLocalAddress(imageName)
	if err != nil {
		return nil, err
	}
	return pushOCIImage(a, imageLocalAddress, imageName, tag, localSourceTgzPath)
}

// DeleteImage deletes the OCI image from the remote registry, including all
// versions and tags.
func (a *ArtifactRegistryOCIProvider) DeleteImage(imageName, digest string) error {
	return a.deleteImage(imageName, digest)
}

// ArtifactRegistryHelmProvider provides methods for interacting with the test registry-server
// using the helm-sync interface.
type ArtifactRegistryHelmProvider struct {
	ArtifactRegistryProvider

	HelmClient *testshell.HelmClient
}

// Client for executing helm and crane commands.
func (a *ArtifactRegistryHelmProvider) Client() HelmClient {
	return a.HelmClient
}

// Login to the registry with the HelmClient
func (a *ArtifactRegistryHelmProvider) Login() error {
	registryHost := a.registryHost()
	if err := a.HelmClient.Login(registryHost); err != nil {
		return fmt.Errorf("HelmClient.Login(%q): %w", registryHost, err)
	}
	return nil
}

// Logout of the registry with the HelmClient
func (a *ArtifactRegistryHelmProvider) Logout() error {
	registryHost := a.registryHost()
	if err := a.HelmClient.Logout(registryHost); err != nil {
		return fmt.Errorf("HelmClient.Logout(%q): %w", registryHost, err)
	}
	return nil
}

// Setup performs required setup for the helm AR provider.
// This requires setting up authentication for the helm CLI.
func (a *ArtifactRegistryHelmProvider) Setup() error {
	return a.createRepository()
}

// RepositoryLocalURL is the repositoryLocalAddress prepended with oci://
// For pushing with the local helm client.
func (a *ArtifactRegistryHelmProvider) RepositoryLocalURL() (string, error) {
	repoAddr, err := a.repositoryLocalAddress()
	if err != nil {
		return "", err
	}
	return ociURL(repoAddr), nil
}

// RepositoryRemoteURL is the repositoryRemoteAddress prepended with oci://
// For pulling with RSync's `.spec.helm.repo`
func (a *ArtifactRegistryHelmProvider) RepositoryRemoteURL() (string, error) {
	repoAddr, err := a.repositoryRemoteAddress()
	if err != nil {
		return "", err
	}
	return ociURL(repoAddr), nil
}

// PushPackage pushes the local helm chart as an OCI image to the remote registry.
func (a *ArtifactRegistryHelmProvider) PushPackage(localChartTgzPath string) (*HelmPackage, error) {
	repoLocalURL, err := a.RepositoryLocalURL()
	if err != nil {
		return nil, err
	}
	pkg, err := pushHelmPackage(a, repoLocalURL, localChartTgzPath)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}

// DeletePackage deletes the helm chart OCI image from the remote registry,
// including all versions and tags.
func (a *ArtifactRegistryHelmProvider) DeletePackage(chartName, digest string) error {
	return a.deleteImage(chartName, digest)
}
