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

package controllers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/core/k8sobjects"
)

func TestGithubAppFromSecret(t *testing.T) {
	testCases := map[string]struct {
		secretData        map[string][]byte
		expectedGithubApp githubAppSpec
	}{
		"should parse the correct data keys for client ID": {
			secretData: map[string][]byte{
				"github-app-client-id":       []byte("client-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			expectedGithubApp: githubAppSpec{
				clientID:       "client-id-0",
				installationID: "installation-id-0",
			},
		},
		"should parse the correct data keys for application ID": {
			secretData: map[string][]byte{
				"github-app-application-id":  []byte("application-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			expectedGithubApp: githubAppSpec{
				appID:          "application-id-0",
				installationID: "installation-id-0",
			},
		},
		"should parse the correct data keys for optional base URL": {
			secretData: map[string][]byte{
				"github-app-application-id":  []byte("application-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
				"github-app-base-url":        []byte("base-url-0"),
			},
			expectedGithubApp: githubAppSpec{
				appID:          "application-id-0",
				installationID: "installation-id-0",
				baseURL:        "base-url-0",
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			secretObject := k8sobjects.SecretObject("foo")
			secretObject.Data = tc.secretData
			assert.Equal(t, tc.expectedGithubApp, githubAppFromSecret(secretObject))
		})
	}
}

func TestGitSyncEnvVars(t *testing.T) {
	privateKeyEnvVar := corev1.EnvVar{
		Name: "GITSYNC_GITHUB_APP_PRIVATE_KEY",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "foo",
				},
				Key: "github-app-private-key",
			},
		},
	}
	testCases := map[string]struct {
		secretName      string
		githubApp       githubAppSpec
		expectedEnvVars []corev1.EnvVar
	}{
		"empty githubapp should return empty env vars": {
			secretName: "foo",
			githubApp:  githubAppSpec{},
			expectedEnvVars: []corev1.EnvVar{
				privateKeyEnvVar,
			},
		},
		"githubapp should map to the expected git-sync env vars for client ID": {
			secretName: "foo",
			githubApp: githubAppSpec{
				clientID:       "client-id-0",
				installationID: "installation-id-0",
			},
			expectedEnvVars: []corev1.EnvVar{
				privateKeyEnvVar,
				{
					Name:  "GITSYNC_GITHUB_APP_CLIENT_ID",
					Value: "client-id-0",
				},
				{
					Name:  "GITSYNC_GITHUB_APP_INSTALLATION_ID",
					Value: "installation-id-0",
				},
			},
		},
		"githubapp should map to the expected git-sync env vars for application ID": {
			secretName: "foo",
			githubApp: githubAppSpec{
				appID:          "app-id-0",
				installationID: "installation-id-0",
			},
			expectedEnvVars: []corev1.EnvVar{
				privateKeyEnvVar,
				{
					Name:  "GITSYNC_GITHUB_APP_APPLICATION_ID",
					Value: "app-id-0",
				},
				{
					Name:  "GITSYNC_GITHUB_APP_INSTALLATION_ID",
					Value: "installation-id-0",
				},
			},
		},
		"githubapp should map to the expected git-sync env vars for optional base URL": {
			secretName: "foo",
			githubApp: githubAppSpec{
				appID:          "app-id-0",
				installationID: "installation-id-0",
				baseURL:        "base-url-0",
			},
			expectedEnvVars: []corev1.EnvVar{
				privateKeyEnvVar,
				{
					Name:  "GITSYNC_GITHUB_APP_APPLICATION_ID",
					Value: "app-id-0",
				},
				{
					Name:  "GITSYNC_GITHUB_APP_INSTALLATION_ID",
					Value: "installation-id-0",
				},
				{
					Name:  "GITSYNC_GITHUB_BASE_URL",
					Value: "base-url-0",
				},
			},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.expectedEnvVars, tc.githubApp.GitSyncEnvVars(tc.secretName))
		})
	}
}
