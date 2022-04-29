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
	"fmt"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"go.uber.org/multierr"
	"kpt.dev/configsync/e2e"
)

const (
	bitbucketProject = "CSCI"

	// PrivateSSHKey is secret name of the private SSH key stored in the Cloud Secret Manager.
	PrivateSSHKey = "config-sync-ci-ssh-private-key"

	// secretManagerProject is the project id of the Secret Manager that stores the secrets.
	secretManagerProject = "stolos-dev"

	repoNameMaxLength = 62
)

// BitbucketClient is the client that calls the Bitbucket REST APIs.
type BitbucketClient struct {
	oauthKey     string
	oauthSecret  string
	refreshToken string
}

// newBitbucketClient instantiates a new Bitbucket client.
func newBitbucketClient() (*BitbucketClient, error) {
	client := &BitbucketClient{}

	var err error
	if client.oauthKey, err = FetchCloudSecret("bitbucket-oauth-key"); err != nil {
		return client, err
	}
	if client.oauthSecret, err = FetchCloudSecret("bitbucket-oauth-secret"); err != nil {
		return client, err
	}
	if client.refreshToken, err = FetchCloudSecret("bitbucket-refresh-token"); err != nil {
		return client, err
	}
	return client, nil
}

// Type returns the provider type.
func (b *BitbucketClient) Type() string {
	return e2e.Bitbucket
}

// RemoteURL returns the Git URL for the Bitbucket repository.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
func (b *BitbucketClient) RemoteURL(_ int, name string) string {
	return b.SyncURL(name)
}

// SyncURL returns a URL for Config Sync to sync from.
// name refers to the repo name in the format of <NAMESPACE>/<NAME> of RootSync|RepoSync.
// The Bitbucket Rest API doesn't allow slash in the repository name, so slash has to be replaced with dash in the name.
func (b *BitbucketClient) SyncURL(name string) string {
	return fmt.Sprintf("git@bitbucket.org:%s/%s", GitUser, strings.ReplaceAll(name, "/", "-"))
}

// CreateRepository calls the POST API to create a remote repository on Bitbucket.
// The remote repo name is unique with a prefix of the local name.
func (b *BitbucketClient) CreateRepository(localName string) (string, error) {
	u, err := uuid.NewRandom()
	if err != nil {
		return "", errors.Wrap(err, "failed to generate a new UUID")
	}
	// Make the remote repoName unique in order to run multiple tests in parallel.
	repoName := strings.ReplaceAll(localName, "/", "-") + "-" + u.String()
	if len(repoName) > repoNameMaxLength {
		repoName = repoName[:repoNameMaxLength]
	}

	// Create a remote repository on demand with a random localName.
	accessToken, err := b.refreshAccessToken()
	if err != nil {
		return "", err
	}

	out, err := exec.Command("curl", "-sX", "POST",
		"-H", "Content-Type: application/json",
		"-H", fmt.Sprintf("Authorization:Bearer %s", accessToken),
		"-d", fmt.Sprintf("{\"scm\": \"git\",\"project\": {\"key\": \"%s\"},\"is_private\": \"true\"}", bitbucketProject),
		fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s", GitUser, repoName)).CombinedOutput()

	if err != nil {
		return "", errors.Wrap(err, string(out))
	}
	if strings.Contains(string(out), "\"type\": \"error\"") {
		return "", errors.New(string(out))
	}
	return repoName, nil
}

// DeleteRepositories calls the DELETE API to delete all remote repositories on Bitbucket.
// It deletes multiple repos in a single function in order to reuse the access_token.
func (b *BitbucketClient) DeleteRepositories(names ...string) error {
	accessToken, err := b.refreshAccessToken()
	if err != nil {
		return err
	}

	return deleteRepos(accessToken, names...)
}

func deleteRepos(accessToken string, names ...string) error {
	var errs error
	for _, name := range names {
		out, err := exec.Command("curl", "-sX", "DELETE",
			"-H", fmt.Sprintf("Authorization:Bearer %s", accessToken),
			fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s/%s",
				GitUser, name)).CombinedOutput()

		if err != nil {
			errs = multierr.Append(errs, errors.Wrap(err, string(out)))
		}
		if len(out) != 0 {
			errs = multierr.Append(errs, errors.New(string(out)))
		}
	}
	return errs
}

// DeleteObsoleteRepos deletes all repos that were created more than 24 hours ago.
func (b *BitbucketClient) DeleteObsoleteRepos() error {
	accessToken, err := b.refreshAccessToken()
	if err != nil {
		return err
	}

	page := 1
	for page != -1 {
		page, err = deleteObsoleteReposByPage(accessToken, page)
		if err != nil {
			return err
		}
	}
	return nil
}

func (b *BitbucketClient) refreshAccessToken() (string, error) {
	out, err := exec.Command("curl", "-sX", "POST", "-u",
		fmt.Sprintf("%s:%s", b.oauthKey, b.oauthSecret),
		"https://bitbucket.org/site/oauth2/access_token",
		"-d", "grant_type=refresh_token",
		"-d", "refresh_token="+b.refreshToken).CombinedOutput()

	if err != nil {
		return "", errors.Wrap(err, string(out))
	}

	var output map[string]interface{}
	err = json.Unmarshal(out, &output)
	if err != nil {
		return "", err
	}

	accessToken, ok := output["access_token"]
	if !ok {
		return "", fmt.Errorf("no access_token: %s", string(out))
	}

	return accessToken.(string), nil
}

// FetchCloudSecret fetches secret from Google Cloud Secret Manager.
func FetchCloudSecret(name string) (string, error) {
	out, err := exec.Command("gcloud", "secrets", "versions",
		"access", "latest", "--project", secretManagerProject, "--secret", name).CombinedOutput()
	if err != nil {
		return "", errors.Wrap(err, string(out))
	}
	return string(out), nil
}

func deleteObsoleteReposByPage(accessToken string, page int) (int, error) {
	out, err := exec.Command("curl", "-sX", "GET",
		"-H", fmt.Sprintf("Authorization:Bearer %s", accessToken),
		fmt.Sprintf("https://api.bitbucket.org/2.0/repositories/%s?page=%d",
			GitUser, page)).CombinedOutput()
	if err != nil {
		return -1, errors.Wrap(err, string(out))
	}
	repos, page, err := filterObsoleteRepos(out)
	if err != nil {
		return -1, err
	}

	fmt.Println("Deleting the following repos: ", strings.Join(repos, ", "))
	err = deleteRepos(accessToken, repos...)
	return page, err
}

// filterObsoleteRepos extracts the names of the repos that were created more than 24 hours ago.
func filterObsoleteRepos(bytes []byte) ([]string, int, error) {
	nextPage := -1
	var response interface{}
	if err := json.Unmarshal(bytes, &response); err != nil {
		return nil, nextPage, err
	}

	m := response.(map[string]interface{})
	next, found := m["next"]
	if found {
		u, err := url.Parse(next.(string))
		if err != nil {
			return nil, nextPage, err
		}
		query, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return nil, nextPage, err
		}
		nextPage, err = strconv.Atoi(query.Get("page"))
		if err != nil {
			return nil, nextPage, err
		}
	}

	values, found := m["values"]
	if !found {
		return nil, nextPage, errors.New("no repos returned")
	}
	var result []string
	for _, r := range values.([]interface{}) {
		repo := r.(map[string]interface{})
		name, found := repo["name"]
		if !found {
			return nil, nextPage, errors.New("no name returned")
		}
		updatedOn, found := repo["updated_on"]
		if !found {
			return nil, nextPage, errors.New("no updated_on returned")
		}
		updatedTime, err := time.Parse(time.RFC3339, updatedOn.(string))
		if err != nil {
			return nil, nextPage, err
		}
		// Only list those repos that were created more than 24 hours ago
		if time.Now().After(updatedTime.Add(24 * time.Hour)) {
			result = append(result, name.(string))
		}
	}
	return result, nextPage, nil
}
