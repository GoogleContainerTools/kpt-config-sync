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

package controllers

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
)

func TestValidateSecretExist(t *testing.T) {
	testCases := []struct {
		name            string
		secretReference string
		secretNamespace string
		wantError       bool
		wantSecret      *corev1.Secret
	}{
		{
			name:            "Secret present",
			secretNamespace: "bookinfo",
			secretReference: "ssh-key",
			wantSecret: secretObj(t, "ssh-key", configsync.AuthSSH, configsync.GitSource,
				core.Namespace("bookinfo"),
				core.UID("1"), core.ResourceVersion("1"), core.Generation(1),
			),
		},

		{
			name:            "Secret not present",
			secretNamespace: "bookinfo",
			secretReference: "ssh-key-root",
			wantError:       true,
		},
	}

	ctx := context.Background()
	fakeClient := syncerFake.NewClient(t, core.Scheme, secretObj(t, "ssh-key", configsync.AuthSSH, configsync.GitSource, core.Namespace("bookinfo")))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret, err := validateSecretExist(ctx, tc.secretReference, tc.secretNamespace, fakeClient)
			if tc.wantError && err == nil {
				t.Errorf("validateSecretExist() got error: %q, want error", err)
			} else if !tc.wantError && err != nil {
				t.Errorf("validateSecretExist() got error: %q, want error: nil", err)
			}
			if !tc.wantError {
				fakeClient.Scheme().Default(tc.wantSecret)
				if diff := cmp.Diff(secret, tc.wantSecret); diff != "" {
					t.Errorf("mutateRepoSyncDeployment() got diff: %v\nwant: nil", diff)
				}
			}
		})
	}
}

func TestValidateSecretData(t *testing.T) {
	testCases := map[string]struct {
		auth       configsync.AuthType
		secretData map[string][]byte
		wantError  error
	}{
		"SSH auth data present": {
			auth: configsync.AuthSSH,
			secretData: map[string][]byte{
				"ssh": []byte("ssh-key-0"),
			},
		},
		"Cookiefile auth data present": {
			auth: configsync.AuthCookieFile,
			secretData: map[string][]byte{
				"cookie_file": []byte("cookiefile-0"),
			},
		},
		"None auth": {
			auth: configsync.AuthNone,
		},
		"GCENode auth": {
			auth: configsync.AuthGCENode,
		},
		"Github App auth with client ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-client-id":       []byte("client-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
		},
		"Github App auth with application ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-application-id":  []byte("application-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
		},
		"Github App auth with optional base URL": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-application-id":  []byte("application-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
				"github-app-base-url":        []byte("base-url-0"),
			},
		},
		"Invalid Github App auth with missing private key": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-client-id":       []byte("client-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			wantError: fmt.Errorf(`git secretType was set as "githubapp" but github-app-private-key key is not present in foo secret`),
		},
		"Invalid Github App auth with missing installation ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key": []byte("private-key-0"),
				"github-app-client-id":   []byte("client-id-0"),
			},
			wantError: fmt.Errorf(`git secretType was set as "githubapp" but github-app-installation-id key is not present in foo secret`),
		},
		"Invalid Github App auth with client ID AND application ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-client-id":       []byte("client-id-0"),
				"github-app-application-id":  []byte("application-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			wantError: fmt.Errorf(`git secretType was set to "githubapp" but more than one of (github-app-application-id, github-app-client-id) is present in foo secret`),
		},
		"Invalid Github App auth with neither client ID NOR application ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			wantError: fmt.Errorf(`git secretType was set to "githubapp" but one of (github-app-application-id, github-app-client-id) is not present in foo secret`),
		},
		"Invalid Github App auth with empty client ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-client-id":       []byte(""),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			wantError: fmt.Errorf(`git secretType was set to "githubapp" but one of (github-app-application-id, github-app-client-id) is not present in foo secret`),
		},
		"Invalid Github App auth with empty application ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-application-id":  []byte(""),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			wantError: fmt.Errorf(`git secretType was set to "githubapp" but one of (github-app-application-id, github-app-client-id) is not present in foo secret`),
		},
		"Invalid Github App auth with empty private key": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte(""),
				"github-app-client-id":       []byte("client-id-0"),
				"github-app-installation-id": []byte("installation-id-0"),
			},
			wantError: fmt.Errorf(`git secretType was set as "githubapp" but github-app-private-key key is not present in foo secret`),
		},
		"Invalid Github App auth with empty installation ID": {
			auth: configsync.AuthGithubApp,
			secretData: map[string][]byte{
				"github-app-private-key":     []byte("private-key-0"),
				"github-app-client-id":       []byte("client-id-0"),
				"github-app-installation-id": []byte(""),
			},
			wantError: fmt.Errorf(`git secretType was set as "githubapp" but github-app-installation-id key is not present in foo secret`),
		},
		"Usupported auth": {
			auth:      "( ͡° ͜ʖ ͡°)",
			wantError: fmt.Errorf(`git secretType is set to unsupported value: "( ͡° ͜ʖ ͡°)"`),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			secretObject := k8sobjects.SecretObject("foo")
			secretObject.Data = tc.secretData
			assert.Equal(t, tc.wantError, validateSecretData(tc.auth, secretObject))
		})
	}
}
