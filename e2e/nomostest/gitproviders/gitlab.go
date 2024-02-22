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
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/multierr"
	"kpt.dev/configsync/e2e"
)

const (
	projectNameMaxLength = 256
	groupID              = 15698791
	groupName            = "configsync"
)

// GitlabClient is the client that will call Gitlab REST APIs.
type GitlabClient struct {
	privateToken string
}

// newGitlabClient instantiates a new GitlabClient.
func newGitlabClient() (*GitlabClient, error) {
	client := &GitlabClient{}

	var err error

	if client.privateToken, err = FetchCloudSecret("gitlab-private-token"); err != nil {
		return client, err
	}
	return client, nil
}

// Type returns the git provider type
func (g *GitlabClient) Type() string {
	return e2e.GitLab
}

// RemoteURL returns the Git URL for the Gitlab project repository.
func (g *GitlabClient) RemoteURL(name string) (string, error) {
	return g.SyncURL(name), nil
}

// SyncURL returns a URL for Config Sync to sync from.
func (g *GitlabClient) SyncURL(name string) string {
	return fmt.Sprintf("git@gitlab.com:%s/%s.git", groupName, name)
}

// CreateRepository calls the POST API to create a project/repository on Gitlab.
// The remote repo name is unique with a prefix of the local name.
func (g *GitlabClient) CreateRepository(name string) (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("failed to generate a new UUID: %w", err)
	}

	repoName := name + "-" + u.String()
	// Gitlab create projects API doesn't allow '/' character
	// so all instances are replaced with '-'
	repoName = strings.ReplaceAll(repoName, "/", "-")
	if len(repoName) > projectNameMaxLength {
		repoName = repoName[:projectNameMaxLength]
	}

	// Projects created under the `configsync` group (namespaceId: 15698791) has
	// no protected branch.
	out, err := exec.Command("curl", "-s", "--request", "POST",
		fmt.Sprintf("https://gitlab.com/api/v4/projects?name=%s&namespace_id=%d&initialize_with_readme=true", repoName, groupID),
		"--header", fmt.Sprintf("PRIVATE-TOKEN: %s", g.privateToken)).CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("%s: %w", string(out), err)
	}
	if !strings.Contains(string(out), fmt.Sprintf("\"name\":\"%s\"", repoName)) {
		return "", errors.New(string(out))
	}

	return repoName, nil
}

// GetProjectID is a helper function for DeleteRepositories
// since Gitlab API only deletes by id
func GetProjectID(g *GitlabClient, name string) (string, error) {
	out, err := exec.Command("curl", "-s", "--request", "GET",
		fmt.Sprintf("https://gitlab.com/api/v4/projects?search=%s", name),
		"--header", fmt.Sprintf("PRIVATE-TOKEN: %s", g.privateToken)).CombinedOutput()

	if err != nil {
		return "", fmt.Errorf("Failure retrieving id for project %s: %w", name, err)
	}

	var response []interface{}

	err = json.Unmarshal(out, &response)
	if err != nil {
		return "", fmt.Errorf("%s: %w", string(out), err)
	}

	var float float64
	var ok bool

	// the assumption is that our project name is unique, so we'll get exactly 1 result
	if len(response) < 1 {
		return "", fmt.Errorf("Project with name %s: %w", name, err)
	}
	if len(response) > 1 {
		return "", fmt.Errorf("Project with name %s is not unique: %w", name, err)
	}
	m := response[0].(map[string]interface{})
	if x, found := m["id"]; found {
		if float, ok = x.(float64); !ok {
			return "", fmt.Errorf("Project id in the respose isn't a float: %w", err)
		}
	} else {
		return "", fmt.Errorf("Project id wasn't found in the response: %w", err)
	}
	id := fmt.Sprintf("%.0f", float)

	return id, nil
}

// DeleteRepositories calls the DELETE API to delete the list of project name in Gitlab.
func (g *GitlabClient) DeleteRepositories(names ...string) error {
	var errs error

	for _, name := range names {
		id, err := GetProjectID(g, name)
		if err != nil {
			errs = multierr.Append(errs, fmt.Errorf("invalid repo name: %w", err))
		} else {
			out, err := exec.Command("curl", "-s", "--request", "DELETE",
				fmt.Sprintf("https://gitlab.com/api/v4/projects/%s", id),
				"--header", fmt.Sprintf("PRIVATE-TOKEN: %s", g.privateToken)).CombinedOutput()

			if err != nil {
				errs = multierr.Append(errs, fmt.Errorf("%s: %w", string(out), err))
			}

			if !strings.Contains(string(out), "\"message\":\"202 Accepted\"") {
				return errors.New(string(out))
			}
		}
	}
	return errs
}

// DeleteObsoleteRepos deletes all projects that has been inactive more than 24 hours
func (g *GitlabClient) DeleteObsoleteRepos() error {
	repos, _ := g.GetObsoleteRepos()

	err := g.DeleteRepoByID(repos...)
	return err
}

// DeleteRepoByID calls the DELETE API to delete the list of project id in Gitlab.
func (g *GitlabClient) DeleteRepoByID(ids ...string) error {
	var errs error

	for _, id := range ids {
		out, err := exec.Command("curl", "-s", "--request", "DELETE",
			fmt.Sprintf("https://gitlab.com/api/v4/projects/%s", id),
			"--header", fmt.Sprintf("PRIVATE-TOKEN: %s", g.privateToken)).CombinedOutput()

		if err != nil {
			errs = multierr.Append(errs, fmt.Errorf("%s: %w", string(out), err))
		}

		if !strings.Contains(string(out), "\"message\":\"202 Accepted\"") {
			return fmt.Errorf("unexpected response in DeleteRepoByID: %s", string(out))
		}
	}
	return errs
}

// GetObsoleteRepos is a helper function to get all project ids that has been inactive more than 24 hours
func (g *GitlabClient) GetObsoleteRepos() ([]string, error) {
	var result []string
	pageNum := 1
	cutOffDate := time.Now().AddDate(0, 0, -1)
	formattedDate := fmt.Sprintf("%d-%02d-%02dT%02d:%02d:%02d",
		cutOffDate.Year(), cutOffDate.Month(), cutOffDate.Day(),
		cutOffDate.Hour(), cutOffDate.Minute(), cutOffDate.Second())

	for {
		out, err := exec.Command("curl", "-s", "--request", "GET",
			fmt.Sprintf("https://gitlab.com/api/v4/projects?last_activity_before=%s&owned=yes&simple=yes&page=%d", formattedDate, pageNum),
			"--header", fmt.Sprintf("PRIVATE-TOKEN: %s", g.privateToken)).CombinedOutput()

		if err != nil {
			return result, fmt.Errorf("Failure retrieving obsolete repos: %w", err)
		}

		if len(out) <= 2 {
			break
		}

		pageNum++
		var response []interface{}

		err = json.Unmarshal(out, &response)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", string(out), err)
		}

		for i := range response {
			m := response[i].(map[string]interface{})
			if flt, found := m["id"]; found {
				var id float64
				var ok bool
				if id, ok = flt.(float64); !ok {
					return result, fmt.Errorf("Project id in the response isn't a float: %w", err)
				}
				result = append(result, fmt.Sprintf("%.0f", id))

			} else {
				return result, fmt.Errorf("Project id wasn't found in the response: %w", err)
			}
		}
	}

	return result, nil
}
