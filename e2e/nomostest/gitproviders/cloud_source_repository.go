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

package gitproviders

import (
	"fmt"
	"strings"

	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/gitproviders/util"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testshell"
)

// CSRReaderEmail returns the email of the google service account with
// permission to read from Cloud Source Registry.
func CSRReaderEmail() string {
	return fmt.Sprintf("e2e-test-csr-reader@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

// CSRClient is the client that interacts with Google Cloud Source Repository.
type CSRClient struct {
	// project in which to store the source repo
	project string
	// repoPrefix is used to avoid overlap
	repoPrefix string
	// shell used for invoking CLI tools
	shell *testshell.TestShell
}

// newCSRClient instantiates a new CSR client.
func newCSRClient(repoPrefix string, shell *testshell.TestShell) *CSRClient {
	return &CSRClient{
		project:    *e2e.GCPProject,
		repoPrefix: repoPrefix,
		shell:      shell,
	}
}

func (c *CSRClient) fullName(name string) string {
	return util.SanitizeRepoName("cs-e2e-"+c.repoPrefix, name)
}

// Type returns the provider type.
func (c *CSRClient) Type() string {
	return e2e.CSR
}

// RemoteURL returns the Git URL for the CSR repository.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (c *CSRClient) RemoteURL(name string) (string, error) {
	return c.SyncURL(name), nil
}

// SyncURL returns a URL for Config Sync to sync from.
func (c *CSRClient) SyncURL(name string) string {
	return fmt.Sprintf("%s/p/%s/r/%s", testing.CSRHost, c.project, name)
}

func (c *CSRClient) login() error {
	_, err := c.shell.ExecWithDebug("gcloud", "init")
	if err != nil {
		return fmt.Errorf("authorizing gcloud: %w", err)
	}
	return nil
}

// CreateRepository calls the gcloud SDK to create a remote repository on CSR.
// It returns the full name with a prefix.
func (c *CSRClient) CreateRepository(name string) (string, error) {
	fullName := c.fullName(name)
	if err := c.login(); err != nil {
		return fullName, err
	}
	out, err := c.shell.ExecWithDebug("gcloud", "source", "repos",
		"describe", fullName,
		"--project", c.project)
	if err == nil {
		return fullName, nil // repo already exists, skip creation
	}
	if !strings.Contains(string(out), "NOT_FOUND") {
		return fullName, fmt.Errorf("describing source repository: %w", err)
	}

	_, err = c.shell.ExecWithDebug("gcloud", "source", "repos",
		"create", fullName,
		"--project", c.project)
	if err != nil {
		return fullName, fmt.Errorf("creating source repository: %w", err)
	}
	return fullName, nil
}

// DeleteRepositories calls the gcloud SDK to delete the provided repositories from CSR.
func (c *CSRClient) DeleteRepositories(names ...string) error {
	for _, name := range names {
		_, err := c.shell.ExecWithDebug("gcloud", "source", "repos",
			"delete", name,
			"--project", c.project)
		if err != nil {
			return fmt.Errorf("deleting source repository: %w", err)
		}
	}
	return nil
}

// DeleteObsoleteRepos is a no-op because CSR repo names are determined by the
// test cluster name and RSync namespace and name, so it can be reused if it
// failed to be deleted after the test.
func (c *CSRClient) DeleteObsoleteRepos() error {
	return nil
}
