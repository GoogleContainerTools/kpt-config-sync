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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
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
			wantSecret:      secretObj(t, "ssh-key", configsync.AuthSSH, v1beta1.GitSource, core.Namespace("bookinfo")),
		},

		{
			name:            "Secret not present",
			secretNamespace: "bookinfo",
			secretReference: "ssh-key-root",
			wantError:       true,
		},
	}

	ctx := context.Background()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	fakeClient := syncerFake.NewClient(t, s, secretObj(t, "ssh-key", configsync.AuthSSH, v1beta1.GitSource, core.Namespace("bookinfo")))

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			secret, err := validateSecretExist(ctx, tc.secretReference, tc.secretNamespace, fakeClient)
			if tc.wantError && err == nil {
				t.Errorf("validateSecretExist() got error: %q, want error", err)
			} else if !tc.wantError && err != nil {
				t.Errorf("validateSecretExist() got error: %q, want error: nil", err)
			}
			if !tc.wantError {
				if diff := cmp.Diff(secret, tc.wantSecret); diff != "" {
					t.Errorf("mutateRepoSyncDeployment() got diff: %v\nwant: nil", diff)
				}
			}
		})
	}
}

func TestValidateSecretData(t *testing.T) {
	testCases := []struct {
		name      string
		auth      configsync.AuthType
		secret    *corev1.Secret
		wantError bool
	}{
		{
			name:   "SSH auth data present",
			auth:   configsync.AuthSSH,
			secret: secretObj(t, "ssh-key", configsync.AuthSSH, v1beta1.GitSource, core.Namespace("bookinfo")),
		},
		{
			name:   "Cookiefile auth data present",
			auth:   configsync.AuthCookieFile,
			secret: secretObj(t, "ssh-key", "cookie_file", v1beta1.GitSource, core.Namespace("bookinfo")),
		},
		{
			name: "None auth",
			auth: configsync.AuthNone,
		},
		{
			name: "GCENode auth",
			auth: configsync.AuthGCENode,
		},
		{
			name:      "Usupported auth",
			auth:      "( ͡° ͜ʖ ͡°)",
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSecretData(tc.auth, tc.secret)
			if tc.wantError && err == nil {
				t.Errorf("validateSecretData() got error: %q, want error", err)
			} else if !tc.wantError && err != nil {
				t.Errorf("validateSecretData() got error: %q, want error: nil", err)
			}
		})
	}
}
