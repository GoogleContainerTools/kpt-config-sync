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

	corev1 "k8s.io/api/core/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/validate/rsync/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// validateSecretExist validate the presence of secret in the cluster
func validateSecretExist(ctx context.Context, secretRef, namespace string, c client.Client) (*corev1.Secret, error) {
	sRef := client.ObjectKey{
		Name:      secretRef,
		Namespace: namespace,
	}

	secret := &corev1.Secret{}
	if err := c.Get(ctx, sRef, secret); err != nil {
		return nil, err
	}
	return secret, nil
}

// validateSecretData verify secret data for the given auth type.
func validateSecretData(auth configsync.AuthType, secret *corev1.Secret) status.Error {
	hasDataKey := func(key string) bool {
		val, ok := secret.Data[key]
		return ok && len(val) > 0
	}
	switch auth {
	case configsync.AuthSSH:
		if _, ok := secret.Data[GitSecretConfigKeySSH]; !ok {
			return validate.MissingKeyInAuthSecret(auth, GitSecretConfigKeySSH, secret.Name)
		}
	case configsync.AuthCookieFile:
		if _, ok := secret.Data[GitSecretConfigKeyCookieFile]; !ok {
			return validate.MissingKeyInAuthSecret(auth, GitSecretConfigKeyCookieFile, secret.Name)
		}
	case configsync.AuthToken:
		if _, ok := secret.Data[GitSecretConfigKeyToken]; !ok {
			return validate.MissingKeyInAuthSecret(auth, GitSecretConfigKeyToken, secret.Name)
		}
		if _, ok := secret.Data[GitSecretConfigKeyTokenUsername]; !ok {
			return validate.MissingKeyInAuthSecret(auth, GitSecretConfigKeyTokenUsername, secret.Name)
		}
	case configsync.AuthGithubApp:
		if !hasDataKey(GitSecretGithubAppPrivateKey) {
			return validate.MissingKeyInAuthSecret(auth, GitSecretGithubAppPrivateKey, secret.Name)
		}
		if !hasDataKey(GitSecretGithubAppInstallationID) {
			return validate.MissingKeyInAuthSecret(auth, GitSecretGithubAppInstallationID, secret.Name)
		}
		hasAppID := hasDataKey(GitSecretGithubAppApplicationID)
		hasClientID := hasDataKey(GitSecretGithubAppClientID)
		if !(hasAppID || hasClientID) {
			return validate.MissingGithubAppIDInSecret(secret.Name)
		}
		if hasAppID && hasClientID {
			return validate.AmbiguousGithubAppIDInSecret(secret.Name)
		}
	case configsync.AuthNone:
	case configsync.AuthGCENode:
	default:
		return validate.InvalidAuthType(auth)
	}
	return nil
}
