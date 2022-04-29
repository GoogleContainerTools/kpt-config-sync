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

package gitproviders

import (
	"fmt"

	"kpt.dev/configsync/e2e"
)

// LocalProvider refers to the test git-server running on the same test cluster.
type LocalProvider struct{}

// Type returns the provider type.
func (l *LocalProvider) Type() string {
	return e2e.Local
}

// RemoteURL returns the Git URL for connecting to the test git-server.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalProvider) RemoteURL(port int, name string) string {
	return fmt.Sprintf("ssh://git@localhost:%d/git-server/repos/%s", port, name)
}

// SyncURL returns a URL for Config Sync to sync from.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (l *LocalProvider) SyncURL(name string) string {
	return fmt.Sprintf("git@test-git-server.config-management-system-test:/git-server/repos/%s", name)
}

// CreateRepository returns the local name as the remote repo name.
// It is a no-op for the test git-server because all repos are
// initialized at once in git-server.go.
func (l *LocalProvider) CreateRepository(name string) (string, error) {
	return name, nil
}

// DeleteRepositories is a no-op for the test git-server because the git-server
// will be deleted after the test.
func (l *LocalProvider) DeleteRepositories(...string) error {
	return nil
}

// DeleteObsoleteRepos is a no-op for the test git-server because the git-server
// will be deleted after the test.
func (l *LocalProvider) DeleteObsoleteRepos() error {
	return nil
}
