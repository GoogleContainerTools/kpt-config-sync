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
	"kpt.dev/configsync/e2e/nomostest/portforwarder"
	"kpt.dev/configsync/e2e/nomostest/testshell"
)

// LocalProvider refers to the test registry-server running on the same test cluster.
type LocalProvider struct {
	// PortForwarder is a port forwarder to the in-cluster registry server.
	// This is used to communicate from the tests to the in-cluster registry server.
	PortForwarder *portforwarder.PortForwarder
	// repositoryName is the name of the repository. For LocalProvider,
	// this doesn't require explicit repository creation but is just part of the
	// path.
	repositoryName string
	// repositorySuffix is a suffix appended after the repository name. This enables
	// separating different artifact types within the repository
	// (e.g. helm chart vs basic oci image). This is not strictly necessary but
	// provides a guardrail to help prevent gotchas when mixing usage of helm-sync
	// and oci-sync.
	repositorySuffix string
}

// Type returns the provider type.
func (l *LocalProvider) Type() string {
	return e2e.Local
}

// Setup performs setup for LocalProvider (no-op)
func (l *LocalProvider) Setup() error {
	// Local registry is created with the test environment.
	// So no setup is necessary.
	return nil
}

// Teardown performs teardown for LocalProvider (no-op)
func (l *LocalProvider) Teardown() error {
	// Local registry is deleted with the test environment.
	// So no cleanup is necessary.
	return nil
}

// Login to the local registry (no-op)
func (l *LocalProvider) Login() error {
	// Local registry does not require authentication
	return nil
}

// Logout of the local registry (no-op)
func (l *LocalProvider) Logout() error {
	// Local registry does not require authentication
	return nil
}

// repositoryLocalAddress returns the address of the repository in the
// registry for use by local clients, like the helm or crane clients.
func (l *LocalProvider) repositoryLocalAddress() (string, error) {
	if l.PortForwarder == nil {
		return "", fmt.Errorf("PortForwarder must be set for LocalProvider.repositoryLocalAddress()")
	}
	localPort, err := l.PortForwarder.LocalPort()
	if err != nil {
		return "", err
	}
	return l.repositoryLocalAddressWithProxy(l.ProxyAddress(localPort)), nil
}

// repositoryLocalAddressWithProxy returns address of the repository using the
// specified proxy address.
func (l *LocalProvider) repositoryLocalAddressWithProxy(proxyAddress string) string {
	return fmt.Sprintf("%s/%s/%s", proxyAddress, l.repositoryName, l.repositorySuffix)
}

// repositoryRemoteAddress returns the address of the repository in the
// registry for use by remote clients, like Kubernetes or Config Sync.
func (l *LocalProvider) repositoryRemoteAddress() (string, error) {
	return fmt.Sprintf("test-registry-server.test-registry-system/%s/%s", l.repositoryName, l.repositorySuffix), nil
}

// ImageLocalAddress returns the local port forwarded address that proxies to
// the in-cluster git server. For use from the test framework to push to the
// registry.
func (l *LocalProvider) ImageLocalAddress(imageName string) (string, error) {
	if l.PortForwarder == nil {
		return "", fmt.Errorf("PortForwarder must be set for LocalProvider.ImageLocalAddress()")
	}
	localPort, err := l.PortForwarder.LocalPort()
	if err != nil {
		return "", err
	}
	return l.imageLocalAddressWithProxy(l.ProxyAddress(localPort), imageName), nil
}

// imageLocalAddressWithProxy returns address of the image using the specified
// proxy address.
func (l *LocalProvider) imageLocalAddressWithProxy(proxyAddress, imageName string) string {
	return fmt.Sprintf("%s/%s", l.repositoryLocalAddressWithProxy(proxyAddress), imageName)
}

// ProxyAddress returns the localhost address of the proxy with the specified port.
func (l *LocalProvider) ProxyAddress(localPort int) string {
	return fmt.Sprintf("localhost:%d", localPort)
}

// ImageRemoteAddress returns the address of the registry service from within
// the cluster. Fit for use from the *-sync containers inside the cluster.
func (l *LocalProvider) ImageRemoteAddress(imageName string) (string, error) {
	address, err := l.repositoryRemoteAddress()
	if err != nil {
		return "", err
	}
	if imageName != "" {
		address = fmt.Sprintf("%s/%s", address, imageName)
	}
	return address, nil
}

// LocalOCIProvider provides methods for interacting with the test registry-server
// using the oci-sync interface.
type LocalOCIProvider struct {
	LocalProvider

	OCIClient *testshell.OCIClient

	pushedImages map[string]*OCIImage // key = NAME@DIGEST
}

// Client for executing helm and crane commands.
func (l *LocalOCIProvider) Client() CraneClient {
	return l.OCIClient
}

// Restore the proxy and re-push any previously pushed images.
func (l *LocalOCIProvider) Restore(proxyAddress string) error {
	// Logout to configure the local client credentials.
	if err := l.Login(); err != nil {
		return err
	}
	for _, image := range l.pushedImages {
		// Build image address with provided proxy to avoid deadlock waiting for PortForwarder.LocalPort.
		imageLocalAddress := l.imageLocalAddressWithProxy(proxyAddress, image.Name)
		_, err := l.pushImageToAddress(imageLocalAddress, image.Name, image.Tag, image.LocalSourceTgzPath)
		if err != nil {
			return err
		}
	}
	return nil
}

// Reset the state of the registry by deleting images pushed during the test.
func (l *LocalOCIProvider) Reset() error {
	// Delete all charts & images pushed during the test.
	for _, image := range l.pushedImages {
		if err := image.Delete(); err != nil {
			return err
		}
	}
	l.pushedImages = nil
	return nil
}

// PushImage pushes the local tarball as an OCI image to the remote registry.
func (l *LocalOCIProvider) PushImage(imageName, tag, localSourceTgzPath string) (*OCIImage, error) {
	imageLocalAddress, err := l.ImageLocalAddress(imageName)
	if err != nil {
		return nil, err
	}
	return l.pushImageToAddress(imageLocalAddress, imageName, tag, localSourceTgzPath)
}

// pushImageToAddress pushes the local tarball as an OCI image to the specified
// address and record that it was pushed. This allows pushing to the proxy
// without blocking on PortForwarder.LocalPort.
func (l *LocalOCIProvider) pushImageToAddress(imageLocalAddress, imageName, tag, localSourceTgzPath string) (*OCIImage, error) {
	img, err := pushOCIImage(l, imageLocalAddress, imageName, tag, localSourceTgzPath)
	if err != nil {
		return nil, err
	}
	if l.pushedImages == nil {
		l.pushedImages = make(map[string]*OCIImage)
	}
	imageID := fmt.Sprintf("%s@%s", img.Name, img.Digest)
	l.pushedImages[imageID] = img
	return img, nil
}

// DeleteImage deletes the OCI image from the remote registry, including all
// versions and tags.
func (l *LocalOCIProvider) DeleteImage(imageName, digest string) error {
	imageLocalAddress, err := l.ImageLocalAddress(imageName)
	if err != nil {
		return err
	}
	if err := deleteOCIImage(l.OCIClient, imageLocalAddress, digest); err != nil {
		return err
	}
	imageID := fmt.Sprintf("%s@%s", imageName, digest)
	delete(l.pushedImages, imageID)
	return nil
}

// LocalHelmProvider provides methods for interacting with the test registry-server
// using the oci-sync interface.
type LocalHelmProvider struct {
	LocalProvider

	HelmClient *testshell.HelmClient

	pushedPackages map[string]*HelmPackage // key = NAME@DIGEST
}

// Client for executing helm and crane commands.
func (l *LocalHelmProvider) Client() HelmClient {
	return l.HelmClient
}

// RepositoryLocalURLWithPort returns a URL for pushing images to the remote registry.
// localPort refers to the local port the PortForwarder is listening on.
// The name parameter is ignored because helm CLI appends the chart name to the image.
func (l *LocalHelmProvider) RepositoryLocalURLWithPort(localPort int) string {
	return ociURL(l.repositoryLocalAddressWithProxy(l.ProxyAddress(localPort)))
}

// RepositoryLocalURL is the repositoryLocalAddress prepended with oci://
// For pushing with the local helm client.
func (l *LocalHelmProvider) RepositoryLocalURL() (string, error) {
	repoAddr, err := l.repositoryLocalAddress()
	if err != nil {
		return repoAddr, err
	}
	return ociURL(repoAddr), nil
}

// RepositoryRemoteURL is the repositoryRemoteAddress prepended with oci://
// For pulling with RSync's `.spec.helm.repo`
func (l *LocalHelmProvider) RepositoryRemoteURL() (string, error) {
	repoAddr, err := l.repositoryRemoteAddress()
	if err != nil {
		return repoAddr, err
	}
	return ociURL(repoAddr), nil
}

// Restore the proxy and re-push any previously pushed images.
func (l *LocalHelmProvider) Restore(proxyAddress string) error {
	// Logout to configure the local client credentials.
	if err := l.Login(); err != nil {
		return err
	}
	for _, pkg := range l.pushedPackages {
		// Build image address with provided proxy.
		// Calling LocalPort would lead to deadlock.
		repoLocalURL := l.repositoryLocalAddressWithProxy(proxyAddress)
		_, err := l.pushPackageToAddress(repoLocalURL, pkg.LocalChartTgzPath)
		if err != nil {
			return err
		}
	}
	return nil
}

// Reset the state of the registry by deleting images pushed during the test.
func (l *LocalHelmProvider) Reset() error {
	// Delete all charts & images pushed during the test.
	for _, image := range l.pushedPackages {
		if err := image.Delete(); err != nil {
			return err
		}
	}
	l.pushedPackages = nil
	return nil
}

// PushPackage pushes the local helm chart as an OCI image to the remote registry.
func (l *LocalHelmProvider) PushPackage(localChartTgzPath string) (*HelmPackage, error) {
	repoLocalURL, err := l.RepositoryLocalURL()
	if err != nil {
		return nil, err
	}
	return l.pushPackageToAddress(repoLocalURL, localChartTgzPath)
}

// pushPackageToAddress pushes the local helm chart as an OCI image to the
// specified repository URL and records that it was pushed. This allows pushing
// to the proxy without blocking on PortForwarder.LocalPort.
func (l *LocalHelmProvider) pushPackageToAddress(repoLocalURL, localChartTgzPath string) (*HelmPackage, error) {
	pkg, err := pushHelmPackage(l, repoLocalURL, localChartTgzPath)
	if err != nil {
		return nil, err
	}
	if l.pushedPackages == nil {
		l.pushedPackages = make(map[string]*HelmPackage)
	}
	packageID := fmt.Sprintf("%s@%s", pkg.Name, pkg.Digest)
	l.pushedPackages[packageID] = pkg
	return pkg, nil
}

// DeletePackage deletes the helm chart OCI image from the remote registry,
// including all versions and tags.
func (l *LocalHelmProvider) DeletePackage(chartName, digest string) error {
	imageLocalAddress, err := l.ImageLocalAddress(chartName)
	if err != nil {
		return err
	}
	if err := deleteOCIImage(l.HelmClient, imageLocalAddress, digest); err != nil {
		return err
	}
	packageID := fmt.Sprintf("%s@%s", chartName, digest)
	delete(l.pushedPackages, packageID)
	return nil
}
