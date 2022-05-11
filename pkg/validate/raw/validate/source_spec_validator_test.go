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

package validate

import (
	"errors"
	"testing"

	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/testing/fake"
)

func auth(authType string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Auth = authType
	}
}

func ociAuth(authType string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Oci.Auth = authType
	}
}

func named(name string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Name = name
	}
}

func proxy(proxy string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.Proxy = proxy
	}
}

func secret(secretName string) func(*v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.SecretRef.Name = secretName
	}
}

func gcpSAEmail(email string) func(sync *v1beta1.RepoSync) {
	return func(sync *v1beta1.RepoSync) {
		sync.Spec.GCPServiceAccountEmail = email
	}
}

func missingRepo(rs *v1beta1.RepoSync) {
	rs.Spec.Repo = ""
}

func missingImage(rs *v1beta1.RepoSync) {
	rs.Spec.Oci.Image = ""
}

func repoSyncWithGit(opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := fake.RepoSyncObjectV1Beta1("test-ns", configsync.RepoSyncName)
	rs.Spec.SourceType = string(v1beta1.GitSource)
	rs.Spec.Git = &v1beta1.Git{
		Repo: "fake repo",
	}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func repoSyncWithOci(opts ...func(*v1beta1.RepoSync)) *v1beta1.RepoSync {
	rs := fake.RepoSyncObjectV1Beta1("test-ns", configsync.RepoSyncName)
	rs.Spec.SourceType = string(v1beta1.OciSource)
	rs.Spec.Oci = &v1beta1.Oci{
		Image: "fake image",
	}
	for _, opt := range opts {
		opt(rs)
	}
	return rs
}

func TestValidateGitSpec(t *testing.T) {
	testCases := []struct {
		name    string
		obj     *v1beta1.RepoSync
		wantErr status.Error
	}{
		// Validate Git Spec
		{
			name: "valid git",
			obj:  repoSyncWithGit(auth(configsync.AuthNone)),
		},
		{
			name: "a user-defined name",
			obj:  repoSyncWithGit(auth(configsync.AuthNone), named("user-defined-repo-sync-name")),
		},
		{
			name:    "missing git repo",
			obj:     repoSyncWithGit(auth(configsync.AuthNone), missingRepo),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "invalid git auth type",
			obj:     repoSyncWithGit(auth("invalid auth")),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "no op proxy",
			obj:     repoSyncWithGit(auth(configsync.AuthGCENode), proxy("no-op proxy")),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name: "valid proxy with none auth type",
			obj:  repoSyncWithGit(auth(configsync.AuthNone), proxy("ok proxy")),
		},
		{
			name: "valid proxy with cookiefile",
			obj:  repoSyncWithGit(auth(configsync.AuthCookieFile), secret("cookiefile"), proxy("ok proxy")),
		},
		{
			name: "valid proxy with token",
			obj:  repoSyncWithGit(auth(configsync.AuthToken), secret("token"), proxy("ok proxy")),
		},
		{
			name:    "illegal secret",
			obj:     repoSyncWithGit(auth(configsync.AuthNone), secret("illegal secret")),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "missing secret",
			obj:     repoSyncWithGit(auth(configsync.AuthSSH)),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "invalid GCP serviceaccount email",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount), gcpSAEmail("invalid_gcp_sa@gserviceaccount.com")),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "invalid GCP serviceaccount email with correct suffix",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount), gcpSAEmail("foo@my-project.iam.gserviceaccount.com")),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "invalid GCP serviceaccount email without domain",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount), gcpSAEmail("my-project")),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "missing GCP serviceaccount email for git",
			obj:     repoSyncWithGit(auth(configsync.AuthGCPServiceAccount)),
			wantErr: fake.Error(InvalidSyncCode),
		},
		// Validate OCI spec
		{
			name: "valid oci",
			obj:  repoSyncWithOci(ociAuth(configsync.AuthNone)),
		},
		{
			name:    "missing oci image",
			obj:     repoSyncWithOci(ociAuth(configsync.AuthNone), missingImage),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "invalid auth type",
			obj:     repoSyncWithOci(ociAuth("invalid auth")),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "missing GCP serviceaccount email for Oci",
			obj:     repoSyncWithOci(ociAuth(configsync.AuthGCPServiceAccount)),
			wantErr: fake.Error(InvalidSyncCode),
		},
		{
			name:    "invalid source type",
			obj:     fake.RepoSyncObjectV1Beta1("test-ns", configsync.RepoSyncName, fake.WithRepoSyncSourceType("invalid")),
			wantErr: fake.Error(InvalidSyncCode),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := SourceSpec(tc.obj.Spec.SourceType, tc.obj.Spec.Git, tc.obj.Spec.Oci, tc.obj)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Got SourceSpec() error %v, want %v", err, tc.wantErr)
			}
		})
	}
}
