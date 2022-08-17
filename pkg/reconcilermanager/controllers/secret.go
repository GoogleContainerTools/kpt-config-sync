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

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// isUpsertedSecret returns true if the provided secret from the
// config-management-system namespace was upserted by the Reconciler
func isUpsertedSecret(rs *v1beta1.RepoSync, secretName string) bool {
	reconcilerName := core.NsReconcilerName(rs.GetNamespace(), rs.GetName())
	if shouldUpsertCACertSecret(rs) && secretName == ReconcilerResourceName(reconcilerName, rs.Spec.Git.CACertSecretRef.Name) {
		return true
	}
	if shouldUpsertGitSecret(rs) && secretName == ReconcilerResourceName(reconcilerName, rs.Spec.Git.SecretRef.Name) {
		return true
	}
	if shouldUpsertHelmSecret(rs) && secretName == ReconcilerResourceName(reconcilerName, rs.Spec.Helm.SecretRef.Name) {
		return true
	}
	return false
}

func shouldUpsertCACertSecret(rs *v1beta1.RepoSync) bool {
	return v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.GitSource && rs.Spec.Git != nil && useCACert(rs.Spec.CACertSecretRef.Name)
}

func shouldUpsertGitSecret(rs *v1beta1.RepoSync) bool {
	return v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.GitSource && rs.Spec.Git != nil && !SkipForAuth(rs.Spec.Auth)
}

func shouldUpsertHelmSecret(rs *v1beta1.RepoSync) bool {
	return v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.HelmSource && rs.Spec.Helm != nil && !SkipForAuth(rs.Spec.Helm.Auth)
}

// upsertAuthSecret creates or updates the auth secret in the
// config-management-system namespace using an existing secret in the RepoSync
// namespace.
func upsertAuthSecret(ctx context.Context, rs *v1beta1.RepoSync, c client.Client, reconcilerRef types.NamespacedName) (client.ObjectKey, error) {
	rsRef := client.ObjectKeyFromObject(rs)
	switch {
	case shouldUpsertGitSecret(rs):
		nsSecretRef, cmsSecretRef := getSecretRefs(rsRef, reconcilerRef, rs.Spec.Git.SecretRef.Name)
		userSecret, err := getUserSecret(ctx, c, nsSecretRef)
		if err != nil {
			return cmsSecretRef, errors.Wrap(err, "user secret required for git client authentication")
		}
		err = upsertSecret(ctx, c, cmsSecretRef, rsRef, userSecret)
		if err != nil {
			return cmsSecretRef, err
		}
		return cmsSecretRef, nil
	case shouldUpsertHelmSecret(rs):
		nsSecretRef, cmsSecretRef := getSecretRefs(rsRef, reconcilerRef, rs.Spec.Helm.SecretRef.Name)
		userSecret, err := getUserSecret(ctx, c, nsSecretRef)
		if err != nil {
			return cmsSecretRef, errors.Wrap(err, "user secret required for helm client authentication")
		}
		err = upsertSecret(ctx, c, cmsSecretRef, rsRef, userSecret)
		if err != nil {
			return cmsSecretRef, err
		}
		return cmsSecretRef, nil
	default:
		// No secret required
		return client.ObjectKey{}, nil
	}
}

// upsertCACertSecret creates or updates the CA cert secret in the
// config-management-system namespace using an existing secret in the RepoSync
// namespace.
func upsertCACertSecret(ctx context.Context, rs *v1beta1.RepoSync, c client.Client, reconcilerRef types.NamespacedName) (client.ObjectKey, error) {
	rsRef := client.ObjectKeyFromObject(rs)
	if shouldUpsertCACertSecret(rs) {
		nsSecretRef, cmsSecretRef := getSecretRefs(rsRef, reconcilerRef, rs.Spec.Git.CACertSecretRef.Name)
		userSecret, err := getUserSecret(ctx, c, nsSecretRef)
		if err != nil {
			return cmsSecretRef, errors.Wrap(err, "user secret required for git server validation")
		}
		err = upsertSecret(ctx, c, cmsSecretRef, rsRef, userSecret)
		if err != nil {
			return cmsSecretRef, err
		}
		return cmsSecretRef, nil
	}
	// No secret required
	return client.ObjectKey{}, nil
}

func getSecretRefs(rsRef, reconcilerRef client.ObjectKey, secretName string) (nsSecretRef, cmsSecretRef client.ObjectKey) {
	// User managed secret
	nsSecretRef = client.ObjectKey{
		Namespace: rsRef.Namespace,
		Name:      secretName,
	}
	// reconciler-manager managed secret
	cmsSecretRef = client.ObjectKey{
		Namespace: reconcilerRef.Namespace,
		Name:      ReconcilerResourceName(reconcilerRef.Name, secretName),
	}
	return nsSecretRef, cmsSecretRef
}

// getUserSecret gets a user managed secret in the same namespace as the RepoSync.
func getUserSecret(ctx context.Context, c client.Client, nsSecretRef client.ObjectKey) (*corev1.Secret, error) {
	nsSecret := &corev1.Secret{}
	if err := getSecret(ctx, c, nsSecretRef, nsSecret); err != nil {
		if apierrors.IsNotFound(err) {
			return nsSecret, errors.Errorf(
				"secret %s not found", nsSecretRef)
		}
		return nsSecret, errors.Wrapf(err,
			"secret %s get failed", nsSecretRef)
	}
	return nsSecret, nil
}

// upsertSecret creates or updates a secret in config-management-system
// namespace using an existing user secret.
func upsertSecret(ctx context.Context, c client.Client, cmsSecretRef, rsRef types.NamespacedName, userSecret *corev1.Secret) error {
	cmsSecret := &corev1.Secret{}
	if err := getSecret(ctx, c, cmsSecretRef, cmsSecret); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrapf(err,
				"secret %s get failed", cmsSecretRef)
		}
		// Secret not present in config-management-system namespace.
		// Create one using secret in the RepoSync namespace.
		if err := createSecret(ctx, c, cmsSecretRef, rsRef, userSecret); err != nil {
			return errors.Wrapf(err,
				"secret %s create failed", cmsSecretRef)
		}
		return nil
	}
	// Update the existing secret in config-management-system.
	if err := updateSecret(ctx, c, cmsSecret, userSecret); err != nil {
		return errors.Wrapf(err,
			"secret %s update failed", cmsSecretRef)
	}
	return nil
}

// GetSecretKeys returns the keys that are contained in the Secret.
func GetSecretKeys(ctx context.Context, c client.Client, sRef types.NamespacedName) map[string]bool {
	// namespaceSecret represent secret in reposync.namespace.
	namespaceSecret := &corev1.Secret{}
	if err := getSecret(ctx, c, sRef, namespaceSecret); err != nil {
		return nil
	}
	results := map[string]bool{}
	for k := range namespaceSecret.Data {
		results[k] = true
	}
	return results
}

// getSecret secret using provided namespace and name.
func getSecret(ctx context.Context, c client.Client, sRef types.NamespacedName, secret *corev1.Secret) error {
	return c.Get(ctx, sRef, secret)
}

// createSecret secret get the existing secret in reposync.namespace and use secret.data and
// secret.type to createSecret a new secret in config-management-system namespace.
func createSecret(ctx context.Context, c client.Client, sRef, rsRef types.NamespacedName, namespaceSecret *corev1.Secret) error {
	newSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       kinds.Secret().Kind,
			APIVersion: kinds.Secret().Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: sRef.Namespace,
			Name:      sRef.Name,
			Labels: map[string]string{
				metadata.SyncNamespaceLabel: rsRef.Namespace,
				metadata.SyncNameLabel:      rsRef.Name,
			},
		},
		Type: namespaceSecret.Type,
		Data: namespaceSecret.Data,
	}

	return c.Create(ctx, newSecret)
}

// updateSecret secret fetch the existing secret from the cluster and use secret.data and
// secret.type to create a new secret in config-management-system namespace.
func updateSecret(ctx context.Context, c client.Client, existingsecret *corev1.Secret, namespaceSecret *corev1.Secret) error {
	// Update data and type for the existing secret with values from the secret in
	// reposync.namespace
	existingsecret.Data = namespaceSecret.Data
	existingsecret.Type = namespaceSecret.Type

	return c.Update(ctx, existingsecret)
}

// SkipForAuth returns true if the passed auth is either 'none' or 'gcenode' or
// 'gcpserviceaccount'.
func SkipForAuth(auth configsync.AuthType) bool {
	switch auth {
	case configsync.AuthNone, configsync.AuthGCENode, configsync.AuthGCPServiceAccount:
		return true
	default:
		return false
	}
}
