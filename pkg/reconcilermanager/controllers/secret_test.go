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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
)

const (
	sshAuth         = configsync.AuthSSH
	tokenAuth       = configsync.AuthToken
	gitSource       = v1beta1.GitSource
	helmSource      = v1beta1.HelmSource
	namespaceKey    = "ssh-key"
	tokenSecretName = "token"
	keyData         = "test-key"
	tokenData       = "MWYyZDFlMmU2N2Rm"
	updatedKeyData  = "updated-test-key"
)

func repoSyncWithAuth(ns, name string, auth configsync.AuthType, sourceType v1beta1.SourceType, opts ...core.MetaMutator) *v1beta1.RepoSync {
	result := fake.RepoSyncObjectV1Beta1(ns, name, opts...)
	result.Spec.SourceType = string(sourceType)
	if sourceType == v1beta1.GitSource {
		result.Spec.Git = &v1beta1.Git{
			Auth:      auth,
			SecretRef: v1beta1.SecretReference{Name: "ssh-key"},
		}
	} else if sourceType == v1beta1.HelmSource {
		result.Spec.Helm = &v1beta1.Helm{
			Auth:      auth,
			SecretRef: v1beta1.SecretReference{Name: "token"},
		}
	}
	return result
}

func secret(t *testing.T, name, data string, auth configsync.AuthType, sourceType v1beta1.SourceType, opts ...core.MetaMutator) *corev1.Secret {
	t.Helper()
	result := fake.SecretObject(name, opts...)
	result.Data = secretData(t, data, auth, sourceType)
	result.SetLabels(map[string]string{
		metadata.SyncNamespaceLabel: reposyncNs,
		metadata.SyncNameLabel:      reposyncName,
	})
	return result
}

func secretData(t *testing.T, data string, auth configsync.AuthType, sourceType v1beta1.SourceType) map[string][]byte {
	t.Helper()
	key, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal test key: %v", err)
	}
	if auth == configsync.AuthToken && sourceType == v1beta1.HelmSource {
		return map[string][]byte{
			"username": key,
			"password": key,
		}
	}
	return map[string][]byte{
		string(auth): key,
	}
}

func fakeClient(t *testing.T, objs ...client.Object) *syncerFake.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return syncerFake.NewClient(t, s, objs...)
}

func TestCreate(t *testing.T) {
	testCases := []struct {
		name       string
		reposync   *v1beta1.RepoSync
		client     *syncerFake.Client
		wantError  bool
		wantSecret *corev1.Secret
	}{
		{
			name:     "Secret created for git source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client:   fakeClient(t, secret(t, namespaceKey, keyData, sshAuth, gitSource, core.Namespace(reposyncNs))),
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), keyData, sshAuth, gitSource,
				core.Namespace(v1.NSConfigManagementSystem),
			),
		},
		{
			name:     "Secret updated for git source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client: fakeClient(t, secret(t, namespaceKey, updatedKeyData, sshAuth, gitSource, core.Namespace(reposyncNs)),
				secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), keyData, sshAuth, gitSource, core.Namespace(v1.NSConfigManagementSystem)),
			),
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), updatedKeyData, sshAuth, gitSource,
				core.Namespace(v1.NSConfigManagementSystem),
			),
		},
		{
			name:      "Secret not found for git source",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client:    fakeClient(t),
			wantError: true,
		},
		{
			name:      "Secret not updated, secret not present for git source",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client:    fakeClient(t, secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), keyData, sshAuth, gitSource, core.Namespace(v1.NSConfigManagementSystem))),
			wantError: true,
		},
		{
			name:     "Secret created for helm source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client:   fakeClient(t, secret(t, tokenSecretName, tokenData, tokenAuth, helmSource, core.Namespace(reposyncNs))),
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, tokenSecretName), tokenData, tokenAuth, helmSource,
				core.Namespace(v1.NSConfigManagementSystem),
			),
		},
		{
			name:     "Secret updated for helm source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client: fakeClient(t, secret(t, tokenSecretName, updatedKeyData, tokenAuth, helmSource, core.Namespace(reposyncNs)),
				secret(t, ReconcilerResourceName(nsReconcilerName, tokenSecretName), keyData, tokenAuth, helmSource, core.Namespace(v1.NSConfigManagementSystem)),
			),
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, tokenSecretName), updatedKeyData, tokenAuth, helmSource,
				core.Namespace(v1.NSConfigManagementSystem),
			),
		},
		{
			name:      "Secret not found for helm source",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client:    fakeClient(t),
			wantError: true,
		},
		{
			name:      "Secret not updated, secret not present for helm source",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client:    fakeClient(t, secret(t, ReconcilerResourceName(nsReconcilerName, tokenSecretName), keyData, tokenAuth, helmSource, core.Namespace(v1.NSConfigManagementSystem))),
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := upsertSecret(context.Background(), tc.reposync, tc.client, nsReconcilerName)
			if tc.wantError && err == nil {
				t.Errorf("Create() got error: %q, want error", err)
			} else if !tc.wantError && err != nil {
				t.Errorf("Create() got error: %q, want error: nil", err)
			}
			if !tc.wantError {
				if diff := cmp.Diff(tc.client.Objects[core.IDOf(tc.wantSecret)], tc.wantSecret); diff != "" {
					t.Error(diff)
				}
			}
		})
	}
}
