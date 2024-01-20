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
)

// LocalProvider refers to the test registry-server running on the same test cluster.
type LocalProvider struct {
	// PortForwarder is a port forwarder to the in-cluster registry server.
	// This is used to communicate from the tests to the in-cluster registry server.
	PortForwarder *portforwarder.PortForwarder
}

// Type returns the provider type.
func (l *LocalProvider) Type() string {
	return e2e.Local
}

// LocalOCIProvider provides methods for interacting with the test registry-server
// using the oci-sync interface.
type LocalOCIProvider struct {
	LocalProvider
}

// PushURL returns a URL for pushing images to the remote registry.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalOCIProvider) PushURL(name string) (string, error) {
	if l.PortForwarder == nil {
		return "", fmt.Errorf("PortForwarder must be set for LocalProvider.OCIPushURL()")
	}
	port, err := l.PortForwarder.LocalPort()
	if err != nil {
		return "", err
	}
	return l.PushURLWithPort(port, name), nil
}

// PushURLWithPort returns a URL for pushing images to the remote registry.
// localPort refers to the local port the PortForwarder is listening on.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalOCIProvider) PushURLWithPort(localPort int, name string) string {
	return fmt.Sprintf("localhost:%d/oci/%s", localPort, name)
}

// SyncURL returns a URL for Config Sync to sync from using OCI.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalOCIProvider) SyncURL(name string) string {
	return fmt.Sprintf("test-registry-server.test-registry-system/oci/%s", name)
}
