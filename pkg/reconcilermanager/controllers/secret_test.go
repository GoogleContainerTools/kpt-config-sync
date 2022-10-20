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

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/json"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/testing/fake"
	"sigs.k8s.io/cli-utils/pkg/testutil"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	syncerFake "kpt.dev/configsync/pkg/syncer/syncertest/fake"
)

const (
	sshAuth        = configsync.AuthSSH
	tokenAuth      = configsync.AuthToken
	gitSource      = v1beta1.GitSource
	helmSource     = v1beta1.HelmSource
	gitSecretName  = "ssh-key"
	helmSecretName = "token"
	keyData        = "test-key"
	tokenData      = "MWYyZDFlMmU2N2Rm"
	updatedKeyData = "updated-test-key"
)

var nsReconcilerKey = types.NamespacedName{
	Namespace: v1.NSConfigManagementSystem,
	Name:      nsReconcilerName,
}

func repoSyncWithAuth(ns, name string, auth configsync.AuthType, sourceType v1beta1.SourceType, opts ...core.MetaMutator) *v1beta1.RepoSync {
	result := fake.RepoSyncObjectV1Beta1(ns, name, opts...)
	result.Spec.SourceType = string(sourceType)
	if sourceType == v1beta1.GitSource {
		result.Spec.Git = &v1beta1.Git{
			Auth:      auth,
			SecretRef: &v1beta1.SecretReference{Name: gitSecretName},
		}
	} else if sourceType == v1beta1.HelmSource {
		result.Spec.Helm = &v1beta1.HelmRepoSync{HelmBase: v1beta1.HelmBase{
			Auth:      auth,
			SecretRef: &v1beta1.SecretReference{Name: helmSecretName},
		}}
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
	return syncerFake.NewClient(t, core.Scheme, objs...)
}

func TestCreate(t *testing.T) {
	testCases := []struct {
		name       string
		reposync   *v1beta1.RepoSync
		client     *syncerFake.Client
		wantKey    types.NamespacedName
		wantError  bool
		wantSecret *corev1.Secret
	}{
		{
			name:     "Secret created for git source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client:   fakeClient(t, secret(t, gitSecretName, keyData, sshAuth, gitSource, core.Namespace(reposyncNs))),
			wantKey:  types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, gitSecretName)},
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, gitSecretName), keyData, sshAuth, gitSource,
				core.Namespace(nsReconcilerKey.Namespace),
			),
		},
		{
			name:     "Secret updated for git source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client: fakeClient(t, secret(t, gitSecretName, updatedKeyData, sshAuth, gitSource, core.Namespace(reposyncNs)),
				secret(t, ReconcilerResourceName(nsReconcilerName, gitSecretName), keyData, sshAuth, gitSource,
					core.Namespace(nsReconcilerKey.Namespace)),
			),
			wantKey: types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, gitSecretName)},
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, gitSecretName), updatedKeyData, sshAuth, gitSource,
				core.Namespace(nsReconcilerKey.Namespace),
			),
		},
		{
			name:      "Secret not found for git source",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client:    fakeClient(t),
			wantKey:   types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, gitSecretName)},
			wantError: true,
		},
		{
			name:     "Secret not updated, secret not present for git source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, sshAuth, gitSource),
			client: fakeClient(t, secret(t, ReconcilerResourceName(nsReconcilerName, gitSecretName), keyData, sshAuth, gitSource,
				core.Namespace(nsReconcilerKey.Namespace))),
			wantKey:   types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, gitSecretName)},
			wantError: true,
		},
		{
			name:     "Secret created for helm source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client:   fakeClient(t, secret(t, helmSecretName, tokenData, tokenAuth, helmSource, core.Namespace(reposyncNs))),
			wantKey:  types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, helmSecretName)},
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, helmSecretName), tokenData, tokenAuth, helmSource,
				core.Namespace(nsReconcilerKey.Namespace),
			),
		},
		{
			name:     "Secret updated for helm source",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client: fakeClient(t, secret(t, helmSecretName, updatedKeyData, tokenAuth, helmSource, core.Namespace(reposyncNs)),
				secret(t, ReconcilerResourceName(nsReconcilerName, helmSecretName), keyData, tokenAuth, helmSource,
					core.Namespace(nsReconcilerKey.Namespace)),
			),
			wantKey: types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, helmSecretName)},
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, helmSecretName), updatedKeyData, tokenAuth, helmSource,
				core.Namespace(nsReconcilerKey.Namespace),
			),
		},
		{
			name:      "Secret not found for helm source",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client:    fakeClient(t),
			wantKey:   types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, helmSecretName)},
			wantError: true,
		},
		{
			name:      "Secret not updated, secret not present for helm source",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, tokenAuth, helmSource),
			client:    fakeClient(t, secret(t, ReconcilerResourceName(nsReconcilerName, helmSecretName), keyData, tokenAuth, helmSource, core.Namespace(nsReconcilerKey.Namespace))),
			wantKey:   types.NamespacedName{Namespace: nsReconcilerKey.Namespace, Name: ReconcilerResourceName(nsReconcilerName, helmSecretName)},
			wantError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			log := logr.Discard()
			sKey, err := upsertAuthSecret(context.Background(), log, tc.reposync, tc.client, nsReconcilerKey)
			assert.Equal(t, tc.wantKey, sKey, "unexpected secret key returned")
			if tc.wantError {
				assert.Error(t, err, "expected upsertSecrets to error")
				return
			}
			assert.NoError(t, err, "expected upsertSecrets not to error")
			testutil.AssertEqual(t, tc.client.Objects[core.IDOf(tc.wantSecret)], tc.wantSecret, "unexpected secret contents")
		})
	}
}
