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
	// shell is used for invoking command line utilities
	shell *testshell.TestShell
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
	return nil
}

// Teardown performs teardown for LocalProvider (no-op)
func (l *LocalProvider) Teardown() error {
	return nil
}

// localAddress returns the local port forwarded address that proxies to the
// in-cluster git server. For use from the test framework to push to the registry.
func (l *LocalProvider) localAddress(name string) (string, error) {
	if l.PortForwarder == nil {
		return "", fmt.Errorf("PortForwarder must be set for LocalProvider.localAddress()")
	}
	port, err := l.PortForwarder.LocalPort()
	if err != nil {
		return "", err
	}
	return l.localAddressWithPort(port, name), nil
}

// localAddressWithPort returns the local port forwarded address that proxies to the
// in-cluster git server. For use from the test framework to push to the registry.
// Accepts a port parameter for use from the on ready callback.
func (l *LocalProvider) localAddressWithPort(localPort int, name string) string {
	address := fmt.Sprintf("localhost:%d/%s/%s", localPort, l.repositoryName, l.repositorySuffix)
	if name != "" {
		address = fmt.Sprintf("%s/%s", address, name)
	}
	return address
}

// inClusterAddress returns the address of the registry service from within the
// cluster. Fit for use from the *-sync containers inside the cluster.
func (l *LocalProvider) inClusterAddress(name string) string {
	address := fmt.Sprintf("test-registry-server.test-registry-system/%s/%s", l.repositoryName, l.repositorySuffix)
	if name != "" {
		address = fmt.Sprintf("%s/%s", address, name)
	}
	return address
}

// deleteImage the package from the remote registry, including all versions and tags.
func (l *LocalProvider) deleteImage(name, digest string) error {
	localAddress, err := l.localAddress(name)
	if err != nil {
		return err
	}
	imageTag := fmt.Sprintf("%s@%s", localAddress, digest)
	if _, err := l.shell.ExecWithDebug("crane", "delete", imageTag); err != nil {
		return fmt.Errorf("deleting image: %w", err)
	}
	return nil
}

// LocalOCIProvider provides methods for interacting with the test registry-server
// using the oci-sync interface.
type LocalOCIProvider struct {
	LocalProvider
}

// PushURL returns a URL for pushing images to the remote registry.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalOCIProvider) PushURL(name string) (string, error) {
	return l.localAddress(name)
}

// PushURLWithPort returns a URL for pushing images to the remote registry.
// localPort refers to the local port the PortForwarder is listening on.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalOCIProvider) PushURLWithPort(localPort int, name string) string {
	return l.localAddressWithPort(localPort, name)
}

// SyncURL returns a URL for Config Sync to sync from using OCI.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalOCIProvider) SyncURL(name string) string {
	return l.inClusterAddress(name)
}

// LocalHelmProvider provides methods for interacting with the test registry-server
// using the oci-sync interface.
type LocalHelmProvider struct {
	LocalProvider
}

// PushURL returns a URL for pushing images to the remote registry.
// The name parameter is ignored because helm CLI appends the chart name to the image.
func (l *LocalHelmProvider) PushURL(_ string) (string, error) {
	localAddress, err := l.localAddress("")
	if err != nil {
		return localAddress, err
	}
	return fmt.Sprintf("oci://%s", localAddress), nil
}

// PushURLWithPort returns a URL for pushing images to the remote registry.
// localPort refers to the local port the PortForwarder is listening on.
// The name parameter is ignored because helm CLI appends the chart name to the image.
func (l *LocalHelmProvider) PushURLWithPort(localPort int, _ string) string {
	return fmt.Sprintf("oci://%s", l.localAddressWithPort(localPort, ""))
}

// SyncURL returns a URL for Config Sync to sync from using helm.
// The name parameter is ignored because helm CLI appends the chart name to the image.
func (l *LocalHelmProvider) SyncURL(_ string) string {
	return fmt.Sprintf("oci://%s", l.inClusterAddress(""))
}
