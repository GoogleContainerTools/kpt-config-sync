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
	"encoding/json"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
)

const (
	// githubAppSecretManagerSecretName is the name of the secret in GCP Secret Manager
	githubAppSecretManagerSecretName = "github-app-configuration"
)

// RawGithubAppConfiguration represents the JSON format for the githubapp
// configuration stored in Secret Manager.
type RawGithubAppConfiguration struct {
	AppID          string `json:"appID"`
	InstallationID string `json:"installationID"`
	ClientID       string `json:"clientID"`
	PrivateKey     string `json:"privateKey"`
	TestingRepo    string `json:"testingRepo"`
}

func (r RawGithubAppConfiguration) toInternal() *GithubAppConfiguration {
	return &GithubAppConfiguration{
		appID:          r.AppID,
		installationID: r.InstallationID,
		clientID:       r.ClientID,
		privateKey:     r.PrivateKey,
		testingRepo:    r.TestingRepo,
	}
}

// GithubAppConfiguration is the internal representation of the githubapp
// configuration. The fields are private to constrain usage of sensitive values.
type GithubAppConfiguration struct {
	appID          string
	installationID string
	clientID       string
	privateKey     string
	testingRepo    string
}

// FetchGithubAppConfiguration fetches the githubapp configuration and returns
// the internal representation. Uses a local file path if provided, otherwise
// defaults to fetching from Secret Manager.
func FetchGithubAppConfiguration() (*GithubAppConfiguration, error) {
	var outputBytes []byte
	var err error
	if *e2e.GitHubAppConfigFile != "" {
		outputBytes, err = os.ReadFile(*e2e.GitHubAppConfigFile)
	} else {
		var out string
		out, err = FetchCloudSecret(githubAppSecretManagerSecretName)
		outputBytes = []byte(out)
	}
	if err != nil {
		return nil, err
	}
	raw := &RawGithubAppConfiguration{}
	if err := json.Unmarshal(outputBytes, raw); err != nil {
		return nil, fmt.Errorf("encountered error unmarshalling %s secret", githubAppSecretManagerSecretName)
	}
	return raw.toInternal(), nil
}

// Repo returns the repository URL for the githubapp configuration
func (g *GithubAppConfiguration) Repo() string {
	return g.testingRepo
}

// SecretWithClientID returns the githubapp auth Secret using client ID
func (g *GithubAppConfiguration) SecretWithClientID(nn types.NamespacedName) (*corev1.Secret, error) {
	secret := k8sobjects.SecretObject(nn.Name, core.Namespace(nn.Namespace))
	if g.clientID == "" || g.installationID == "" || g.privateKey == "" {
		return nil, fmt.Errorf("missing required values for Secret")
	}
	secret.Data = map[string][]byte{
		controllers.GitSecretGithubAppClientID:       []byte(g.clientID),
		controllers.GitSecretGithubAppInstallationID: []byte(g.installationID),
		controllers.GitSecretGithubAppPrivateKey:     []byte(g.privateKey),
	}
	return secret, nil
}

// SecretWithApplicationID returns the githubapp auth Secret using app ID
func (g *GithubAppConfiguration) SecretWithApplicationID(nn types.NamespacedName) (*corev1.Secret, error) {
	secret := k8sobjects.SecretObject(nn.Name, core.Namespace(nn.Namespace))
	if g.appID == "" || g.installationID == "" || g.privateKey == "" {
		return nil, fmt.Errorf("missing required values for Secret")
	}
	secret.Data = map[string][]byte{
		controllers.GitSecretGithubAppApplicationID:  []byte(g.appID),
		controllers.GitSecretGithubAppInstallationID: []byte(g.installationID),
		controllers.GitSecretGithubAppPrivateKey:     []byte(g.privateKey),
	}
	return secret, nil
}
