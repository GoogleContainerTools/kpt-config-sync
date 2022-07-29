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
	sshAuth        = "ssh"
	namespaceKey   = "ssh-key"
	keyData        = "test-key"
	updatedKeyData = "updated-test-key"
)

func repoSyncWithAuth(ns, name string, auth configsync.AuthType, opts ...core.MetaMutator) *v1beta1.RepoSync {
	result := fake.RepoSyncObjectV1Beta1(ns, name, opts...)
	result.Spec.SourceType = string(v1beta1.GitSource)
	result.Spec.Git = &v1beta1.Git{
		Auth:      auth,
		SecretRef: v1beta1.SecretReference{Name: "ssh-key"},
	}
	return result
}

func secret(t *testing.T, name, data string, auth configsync.AuthType, opts ...core.MetaMutator) *corev1.Secret {
	t.Helper()
	result := fake.SecretObject(name, opts...)
	result.Data = secretData(t, data, auth)
	result.SetLabels(map[string]string{
		metadata.SyncNamespaceLabel: reposyncNs,
		metadata.SyncNameLabel:      reposyncName,
	})
	return result
}

func secretData(t *testing.T, data string, auth configsync.AuthType) map[string][]byte {
	t.Helper()
	key, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("failed to marshal test key: %v", err)
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
			name:     "Secret created",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, sshAuth),
			client:   fakeClient(t, secret(t, namespaceKey, keyData, sshAuth, core.Namespace(reposyncNs))),
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), keyData, sshAuth,
				core.Namespace(v1.NSConfigManagementSystem),
			),
		},
		{
			name:     "Secret updated",
			reposync: repoSyncWithAuth(reposyncNs, reposyncName, sshAuth),
			client: fakeClient(t, secret(t, namespaceKey, updatedKeyData, sshAuth, core.Namespace(reposyncNs)),
				secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), keyData, sshAuth, core.Namespace(v1.NSConfigManagementSystem)),
			),
			wantSecret: secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), updatedKeyData, sshAuth,
				core.Namespace(v1.NSConfigManagementSystem),
			),
		},
		{
			name:      "Secret not found",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, sshAuth),
			client:    fakeClient(t),
			wantError: true,
		},
		{
			name:      "Secret not updated, secret not present",
			reposync:  repoSyncWithAuth(reposyncNs, reposyncName, sshAuth),
			client:    fakeClient(t, secret(t, ReconcilerResourceName(nsReconcilerName, namespaceKey), keyData, sshAuth, core.Namespace(v1.NSConfigManagementSystem))),
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
