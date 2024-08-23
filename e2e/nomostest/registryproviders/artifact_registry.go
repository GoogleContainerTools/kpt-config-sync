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
// This deletes the repository to prevent side effects across executions.
func (a *ArtifactRegistryProvider) Teardown() error {
	out, err := a.gcloudClient.Gcloud("artifacts", "repositories",
		"delete", a.repositoryName,
		"--quiet",
		"--location", a.location,
		"--project", a.project)
	if err != nil {
		if !strings.Contains(string(out), "NOT_FOUND") {
			return fmt.Errorf("failed to delete repository: %w", err)
		}
	}
	return nil
}

// RegistryRemoteAddress returns the remote address of the registry.
func (a *ArtifactRegistryProvider) RegistryRemoteAddress() string {
	return fmt.Sprintf("%s-docker.pkg.dev", a.location)
}

// RepositoryPath returns the name or path of the repository, without the
// registry or image details.
func (a *ArtifactRegistryProvider) RepositoryPath() string {
	return fmt.Sprintf("%s/%s/%s", a.project, a.repositoryName, a.repositorySuffix)
}

// repositoryLocalAddress returns the address of the repository in the
// registry for use by local clients, like the helm or crane clients.
func (a *ArtifactRegistryProvider) repositoryLocalAddress() (string, error) {
	return a.repositoryRemoteAddress(), nil
}

// repositoryRemoteAddress returns the address of the repository in the
// registry for use by remote clients, like Kubernetes or Config Sync.
func (a *ArtifactRegistryProvider) repositoryRemoteAddress() string {
	return fmt.Sprintf("%s/%s", a.RegistryRemoteAddress(), a.RepositoryPath())
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
func (a *ArtifactRegistryProvider) ImageRemoteAddress(imageName string) string {
	return fmt.Sprintf("%s/%s", a.repositoryRemoteAddress(), imageName)
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
	imageAddress := fmt.Sprintf("%s@%s", a.ImageRemoteAddress(imageName), digest)
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

	pushedImages map[string]*OCIImage // key = NAME@DIGEST
}

// Client for executing crane commands.
func (a *ArtifactRegistryOCIProvider) Client() CraneClient {
	return a.OCIClient
}

// Login to the registry with the crane client
func (a *ArtifactRegistryOCIProvider) Login() error {
	registryHost := a.RegistryRemoteAddress()
	if err := a.OCIClient.Login(registryHost); err != nil {
		return fmt.Errorf("OCIClient.Login(%q): %w", registryHost, err)
	}
	return nil
}

// Logout of the registry with the crane client
func (a *ArtifactRegistryOCIProvider) Logout() error {
	registryHost := a.RegistryRemoteAddress()
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
	image, err := pushOCIImage(a, imageLocalAddress, imageName, tag, localSourceTgzPath)
	if err != nil {
		return image, err
	}
	if a.pushedImages == nil {
		a.pushedImages = make(map[string]*OCIImage)
	}
	imageID := fmt.Sprintf("%s@%s", image.Name, image.Digest)
	a.pushedImages[imageID] = image
	return image, nil
}

// DeleteImage deletes the OCI image from the remote registry, including all
// versions and tags.
func (a *ArtifactRegistryOCIProvider) DeleteImage(imageName, digest string) error {
	if err := a.deleteImage(imageName, digest); err != nil {
		return err
	}
	imageID := fmt.Sprintf("%s@%s", imageName, digest)
	delete(a.pushedImages, imageID)
	return nil
}

// Reset the state of the registry by deleting images pushed during the test.
func (a *ArtifactRegistryOCIProvider) Reset() error {
	// Delete all images pushed during the test.
	for _, image := range a.pushedImages {
		if err := image.Delete(); err != nil {
			return err
		}
	}
	a.pushedImages = nil
	return nil
}

// ArtifactRegistryHelmProvider provides methods for interacting with the test registry-server
// using the helm-sync interface.
type ArtifactRegistryHelmProvider struct {
	ArtifactRegistryProvider

	HelmClient *testshell.HelmClient

	pushedPackages map[string]*HelmPackage // key = NAME@DIGEST
}

// Client for executing helm and crane commands.
func (a *ArtifactRegistryHelmProvider) Client() HelmClient {
	return a.HelmClient
}

// Login to the registry with the HelmClient
func (a *ArtifactRegistryHelmProvider) Login() error {
	registryHost := a.RegistryRemoteAddress()
	if err := a.HelmClient.Login(registryHost); err != nil {
		return fmt.Errorf("HelmClient.Login(%q): %w", registryHost, err)
	}
	return nil
}

// Logout of the registry with the HelmClient
func (a *ArtifactRegistryHelmProvider) Logout() error {
	registryHost := a.RegistryRemoteAddress()
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
func (a *ArtifactRegistryHelmProvider) RepositoryRemoteURL() string {
	return ociURL(a.repositoryRemoteAddress())
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
	if a.pushedPackages == nil {
		a.pushedPackages = make(map[string]*HelmPackage)
	}
	packageID := fmt.Sprintf("%s@%s", pkg.Name, pkg.Digest)
	a.pushedPackages[packageID] = pkg
	return pkg, nil
}

// DeletePackage deletes the helm chart OCI image from the remote registry,
// including all versions and tags.
func (a *ArtifactRegistryHelmProvider) DeletePackage(chartName, digest string) error {
	if err := a.deleteImage(chartName, digest); err != nil {
		return err
	}
	packageID := fmt.Sprintf("%s@%s", chartName, digest)
	delete(a.pushedPackages, packageID)
	return nil
}

// Reset the state of the registry by deleting images pushed during the test.
func (a *ArtifactRegistryHelmProvider) Reset() error {
	// Delete all charts pushed during the test.
	for _, chart := range a.pushedPackages {
		if err := chart.Delete(); err != nil {
			return err
		}
	}
	a.pushedPackages = nil
	return nil
}
