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
	"sigs.k8s.io/yaml"
)

// RegistryProvider is an interface for remote OCI registry providers.
// This base interface is common form both OCIRegistryProvider & HelmRegistryProvider.
type RegistryProvider interface {
	Type() string

	// Setup the RegistryProvider for a new test environment.
	Setup() error
	// Teardown the RegistryProvider when stopping a test environment.
	Teardown() error

	// Login to the registry with the client
	Login() error
	// Logout of the registry with the client
	Logout() error

	// Reset the state of the registry by deleting images pushed during the test.
	Reset() error
}

// ProxiedRegistryProvider is a registry provider with a proxy that needs to
// do extra work when the remote server is stopped and started to hide the
// downtime from the client.
type ProxiedRegistryProvider interface {
	RegistryProvider

	// ProxyAddress returns the address of the proxy with the specified port.
	ProxyAddress(localPort int) string
	// Restore the proxy and re-push any previously pushed images.
	//
	// Restore takes the new proxy address to avoid deadlocks trying to ask
	// PortForwarder.LocalPort(), which blocks until after restore is done.
	Restore(proxyAddress string) error
}

// OCIRegistryProvider abstracts remote OCI registry providers for use by OCI clients.
type OCIRegistryProvider interface {
	RegistryProvider

	// RegistryRemoteAddress returns the address of the registry.
	RegistryRemoteAddress() string

	// RepositoryPath returns the name or path of the repository, without the
	// registry or image details.
	RepositoryPath() string

	// ImageLocalAddress returns the local address of the image in the registry.
	ImageLocalAddress(imageName string) (string, error)

	// ImageRemoteAddress returns the address of the image in the registry.
	// For pulling with RSync's `.spec.oci.image`
	ImageRemoteAddress(imageName string) string

	// PushImage pushes a local tarball as an OCI image to the remote registry.
	PushImage(imageName, tag, localSourceTgzPath string) (*OCIImage, error)
	// DeleteImage deletes the OCI image from the remote registry, including all
	// versions and tags.
	DeleteImage(imageName, digest string) error

	// Client for executing crane commands.
	Client() CraneClient
}

// HelmRegistryProvider abstracts remote OCI registry providers for use by Helm clients.
type HelmRegistryProvider interface {
	RegistryProvider

	// RepositoryRemoteURL is the RepositoryRemoteAddress prepended with oci://
	// For pulling with RSync's `.spec.helm.repo`
	RepositoryRemoteURL() string

	// PushPackage pushes a local helm chart as an OCI image to the remote registry.
	PushPackage(localChartTgzPath string) (*HelmPackage, error)
	// DeletePackage deletes the helm chart OCI image from the remote registry,
	// including all versions and tags.
	DeletePackage(imageName, digest string) error

	// Client for executing helm and crane commands.
	Client() HelmClient
}

// CraneClient executes commands with crane
type CraneClient interface {
	Crane(args ...string) ([]byte, error)
}

// HelmClient executes commands with crane or helm
type HelmClient interface {
	Helm(args ...string) ([]byte, error)
	Crane(args ...string) ([]byte, error)
}

// GcloudClient executes commands with gcloud
type GcloudClient interface {
	Gcloud(args ...string) ([]byte, error)
}

// NewOCIProvider creates a RegistryProvider for the specific OCI provider type.
// This enables writing tests that can run against multiple types of registries.
func NewOCIProvider(provider, repoName string, gcloudClient GcloudClient, ociClient *testshell.OCIClient) OCIRegistryProvider {
	repositoryName := fmt.Sprintf("config-sync-e2e-test--%s", repoName)
	repositorySuffix := "oci"
	switch provider {
	case e2e.ArtifactRegistry:
		return &ArtifactRegistryOCIProvider{
			ArtifactRegistryProvider: ArtifactRegistryProvider{
				project:  *e2e.GCPProject,
				location: DefaultLocation,
				// Use cluster name to avoid overlap.
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
				gcloudClient:     gcloudClient,
			},
			OCIClient: ociClient,
		}
	default:
		return &LocalOCIProvider{
			LocalProvider: LocalProvider{
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
			},
			OCIClient: ociClient,
		}
	}
}

// NewHelmProvider creates a RegistryProvider for the specific helm provider type.
// This enables writing tests that can run against multiple types of registries.
func NewHelmProvider(provider, repoName string, gcloudClient GcloudClient, helmClient *testshell.HelmClient) HelmRegistryProvider {
	repositoryName := fmt.Sprintf("config-sync-e2e-test--%s", repoName)
	repositorySuffix := "helm"
	switch provider {
	case e2e.ArtifactRegistry:
		return &ArtifactRegistryHelmProvider{
			ArtifactRegistryProvider: ArtifactRegistryProvider{
				project:  *e2e.GCPProject,
				location: DefaultLocation,
				// Use cluster name to avoid overlap.
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
				gcloudClient:     gcloudClient,
			},
			HelmClient: helmClient,
		}
	default:
		return &LocalHelmProvider{
			LocalProvider: LocalProvider{
				repositoryName:   repositoryName,
				repositorySuffix: repositorySuffix,
			},
			HelmClient: helmClient,
		}
	}
}

// pushOCIImage is a helper that pushes an OCI image with a CraneClient and
// returns the image digest.
func pushOCIImage(provider OCIRegistryProvider, imageLocalAddress, imageName, imageTag, localSourceTgzPath string) (*OCIImage, error) {
	imageWithTag := fmt.Sprintf("%s:%s", imageLocalAddress, imageTag)
	client := provider.Client()
	_, err := client.Crane("append",
		"-f", localSourceTgzPath,
		"-t", imageWithTag)
	if err != nil {
		return nil, fmt.Errorf("pushing image to registry %s: %w", imageWithTag, err)
	}
	out, err := client.Crane("digest", imageWithTag)
	if err != nil {
		return nil, fmt.Errorf("getting digest for %s: %w", imageWithTag, err)
	}
	digest := strings.TrimSpace(string(out))

	return &OCIImage{
		LocalSourceTgzPath: localSourceTgzPath,
		Tag:                imageTag,
		Name:               imageName,
		Digest:             digest,
		Provider:           provider,
	}, nil
}

// deleteOCIImage is a helper that deletes an OCI image with a CraneClient.
func deleteOCIImage(client CraneClient, imageLocalAddress, digest string) error {
	imageAddr := fmt.Sprintf("%s@%s", imageLocalAddress, digest)
	if _, err := client.Crane("delete", imageAddr); err != nil {
		return fmt.Errorf("deleting image from registry %s: %w", imageAddr, err)
	}
	return nil
}

// pushHelmPackage is a helper that pushes a helm chart with a HelmClient.
//
// Note: Helm doesn't provide a way to delete remote package images, so use
// deleteOCIImage instead.
func pushHelmPackage(provider HelmRegistryProvider, repoURL, localChartTgzPath string) (*HelmPackage, error) {
	client := provider.Client()
	if _, err := client.Helm("push", localChartTgzPath, repoURL); err != nil {
		return nil, fmt.Errorf("pushing helm chart: %w", err)
	}

	// Read name and version out of the chart tgz using `helm chart show`.
	out, err := client.Helm("show", "chart", localChartTgzPath)
	if err != nil {
		return nil, fmt.Errorf("reading helm chart metadata: %w", err)
	}
	// Parse output as yaml
	chartManifest := make(map[string]interface{})
	if err := yaml.Unmarshal(out, &chartManifest); err != nil {
		return nil, fmt.Errorf("parsing chart metadata %s: %w",
			localChartTgzPath, err)
	}
	chartName, ok := chartManifest["name"].(string)
	if !ok {
		return nil, fmt.Errorf("parsing chart metadata %s: name field must be a string %+v: %w",
			localChartTgzPath, chartManifest["name"], err)
	}
	chartVersion, ok := chartManifest["version"].(string)
	if !ok {
		return nil, fmt.Errorf("parsing chart metadata %s: version field must be a string %+v: %w",
			localChartTgzPath, chartManifest["version"], err)
	}

	// TODO: parse digest from output of helm push command, to save an API call.
	// Read the image digest from the registry.
	imageWithTag := fmt.Sprintf("%s/%s:%s", strings.TrimPrefix(repoURL, "oci://"), chartName, chartVersion)
	out, err = client.Crane("digest", imageWithTag)
	if err != nil {
		return nil, fmt.Errorf("getting digest: %w", err)
	}
	digest := strings.TrimSpace(string(out))

	return &HelmPackage{
		HelmChartID: HelmChartID{
			Name:    chartName,
			Version: chartVersion,
		},
		LocalChartTgzPath: localChartTgzPath,
		Digest:            digest,
		Provider:          provider,
	}, nil
}
