// Copyright 2025 Google LLC
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

// SSMServiceAccountEmail returns the email of the google service account with
// permission to read from Secure Source Manager.
func SSMServiceAccountEmail() string {
	return fmt.Sprintf("e2e-ssm-reader-sa@%s.iam.gserviceaccount.com", *e2e.GCPProject)
}

// SSMClient is the client that interacts with Google Secure Source Manager.
type SSMClient struct {
	// project in which to store the source repo
	project string
	// project number which is needed for the sync URL of the source repo
	projectNumber string
	// SSM instance ID where the source repo will be stored
	instanceID string
	// region of the SSM instance in which to store the source repo
	region string
	// repoPrefix is used to avoid overlap
	repoPrefix string
	// shell used for invoking CLI tools
	shell *testshell.TestShell
}

var _ GitProvider = &SSMClient{}

// newSSMClient instantiates a new SSM client.
func newSSMClient(repoPrefix string, shell *testshell.TestShell, projectNumber string) *SSMClient {
	return &SSMClient{
		project:       *e2e.GCPProject,
		instanceID:    testing.SSMInstanceID,
		region:        *e2e.SSMInstanceRegion,
		repoPrefix:    repoPrefix,
		shell:         shell,
		projectNumber: projectNumber,
	}
}

func (c *SSMClient) fullName(name string) string {
	return util.SanitizeRepoName(c.repoPrefix, name)
}

// Type returns the provider type.
func (c *SSMClient) Type() string {
	return e2e.SSM
}

// RemoteURL returns the Git URL for the SSM repository.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (c *SSMClient) RemoteURL(name string) (string, error) {
	return c.SyncURL(name), nil
}

// SyncURL returns a URL for Config Sync to sync from.
func (c *SSMClient) SyncURL(name string) string {
	return fmt.Sprintf("https://%s-%s-git.%s.sourcemanager.dev/%s/%s", c.instanceID, c.projectNumber, c.region, c.project, name)
}

func (c *SSMClient) login() error {
	_, err := c.shell.ExecWithDebug("gcloud", "init")
	if err != nil {
		return fmt.Errorf("authorizing gcloud: %w", err)
	}
	return nil
}

// CreateRepository calls the gcloud SDK to create a remote repository on SSM.
// It returns the full name with a prefix.
func (c *SSMClient) CreateRepository(name string) (string, error) {
	fullName := c.fullName(name)
	if err := c.login(); err != nil {
		return fullName, err
	}

	out, err := c.shell.ExecWithDebug("gcloud", "beta", "source-manager", "repos",
		"describe", fullName, "--region", c.region,
		"--project", c.project)
	if err == nil {
		return fullName, nil // repo already exists, skip creation
	}
	if !strings.Contains(string(out), "NOT_FOUND") {
		return fullName, fmt.Errorf("describing source repository: %w", err)
	}

	_, err = c.shell.ExecWithDebug("gcloud", "beta", "source-manager", "repos",
		"create", fullName, "--region", c.region,
		"--instance", c.instanceID,
		"--project", c.project)
	if err != nil {
		return fullName, fmt.Errorf("creating source repository: %w", err)
	}

	return fullName, nil
}

// DeleteRepositories calls the gcloud SDK to delete the provided repositories from SSM.
func (c *SSMClient) DeleteRepositories(names ...string) error {
	for _, name := range names {
		_, err := c.shell.ExecWithDebug("gcloud", "beta", "source-manager", "repos",
			"delete", name,
			"--region", c.region,
			"--project", c.project)
		if err != nil {
			return fmt.Errorf("deleting source repository: %w", err)
		}
	}
	return nil
}

// DeleteObsoleteRepos is a no-op because SSM repo names are determined by the
// test cluster name and RSync namespace and name, so it can be reused if it
// failed to be deleted after the test.
func (c *SSMClient) DeleteObsoleteRepos() error {
	return nil
}
