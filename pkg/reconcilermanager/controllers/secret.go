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
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
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
	if shouldUpsertPrivateCertSecret(rs) && secretName == ReconcilerResourceName(reconcilerName, rs.Spec.Git.PrivateCertSecret.Name) {
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

func shouldUpsertPrivateCertSecret(rs *v1beta1.RepoSync) bool {
	return v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.GitSource && rs.Spec.Git != nil && usePrivateCert(rs.Spec.PrivateCertSecret.Name)
}

func shouldUpsertGitSecret(rs *v1beta1.RepoSync) bool {
	return rs.Spec.SourceType == string(v1beta1.GitSource) && rs.Spec.Git != nil && !SkipForAuth(rs.Spec.Auth)
}

func shouldUpsertHelmSecret(rs *v1beta1.RepoSync) bool {
	return rs.Spec.SourceType == string(v1beta1.HelmSource) && rs.Spec.Helm != nil && !SkipForAuth(rs.Spec.Helm.Auth)
}

// upsertSecrets creates or updates all secrets in config-management-system
// namespace using existing secrets in the reposync.namespace.
func upsertSecrets(ctx context.Context, rs *v1beta1.RepoSync, c client.Client, reconcilerName string) error {
	if shouldUpsertPrivateCertSecret(rs) {
		err := upsertSecret(ctx, rs, c, reconcilerName, rs.Spec.Git.PrivateCertSecret.Name)
		if err != nil {
			return err
		}
	}
	if shouldUpsertGitSecret(rs) {
		err := upsertSecret(ctx, rs, c, reconcilerName, rs.Spec.Git.SecretRef.Name)
		if err != nil {
			return err
		}
	}
	if shouldUpsertHelmSecret(rs) {
		err := upsertSecret(ctx, rs, c, reconcilerName, rs.Spec.Helm.SecretRef.Name)
		if err != nil {
			return err
		}
	}
	return nil
}

// upsertSecret creates or updates a secret in config-management-system
// namespace using an existing secret in the reposync.namespace.
func upsertSecret(ctx context.Context, rs *v1beta1.RepoSync, c client.Client, reconcilerName, nsSecretName string) error {
	// namespaceSecret represent secret in reposync.namespace.
	namespaceSecret := &corev1.Secret{}
	if err := get(ctx, nsSecretName, rs.Namespace, namespaceSecret, c); err != nil {
		if apierrors.IsNotFound(err) {
			return errors.Errorf(
				"%s not found. Create %s secret in %s namespace", nsSecretName, nsSecretName, rs.Namespace)
		}
		return errors.Wrapf(err, "error while retrieving namespace secret")
	}
	// existingsecret represent secret in config-management-system namespace.
	existingsecret := &corev1.Secret{}
	secretName := ReconcilerResourceName(reconcilerName, nsSecretName)
	if err := get(ctx, secretName, v1.NSConfigManagementSystem, existingsecret, c); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrapf(err,
				"failed to get secret %s in namespace %s", secretName, v1.NSConfigManagementSystem)
		}
		// Secret not present in config-management-system namespace. Create one using
		// secret in reposync.namespace.
		if err := create(ctx, namespaceSecret, reconcilerName, c, rs.Name, rs.Namespace); err != nil {
			return errors.Wrapf(err,
				"failed to create %s secret in %s namespace",
				nsSecretName, v1.NSConfigManagementSystem)
		}
		return nil
	}
	// Update the existing secret in config-management-system.
	if err := update(ctx, existingsecret, namespaceSecret, c); err != nil {
		return errors.Wrapf(err, "failed to update the secret %s", existingsecret.Name)
	}
	return nil
}

// GetKeys returns the keys that are contained in the Secret.
func GetKeys(ctx context.Context, c client.Client, secretName, namespace string) map[string]bool {
	// namespaceSecret represent secret in reposync.namespace.
	namespaceSecret := &corev1.Secret{}
	if err := get(ctx, secretName, namespace, namespaceSecret, c); err != nil {
		return nil
	}
	results := map[string]bool{}
	for k := range namespaceSecret.Data {
		results[k] = true
	}
	return results
}

// get secret using provided namespace and name.
func get(ctx context.Context, name, namespace string, secret *corev1.Secret, c client.Client) error {
	// NamespacedName for the secret.
	nn := client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}
	return c.Get(ctx, nn, secret)
}

// create secret get the existing secret in reposync.namespace and use secret.data and
// secret.type to create a new secret in config-management-system namespace.
func create(ctx context.Context, namespaceSecret *corev1.Secret, reconcilerName string, c client.Client, syncName string, syncNamespace string) error {
	newSecret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       kinds.Secret().Kind,
			APIVersion: kinds.Secret().Version,
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				metadata.SyncNamespaceLabel: syncNamespace,
				metadata.SyncNameLabel:      syncName,
			},
		},
	}

	// mutate newSecret with values from the secret in reposync.namespace.
	newSecret.Name = ReconcilerResourceName(reconcilerName, namespaceSecret.Name)
	newSecret.Namespace = v1.NSConfigManagementSystem
	newSecret.Data = namespaceSecret.Data
	newSecret.Type = namespaceSecret.Type

	return c.Create(ctx, newSecret)
}

// update secret fetch the existing secret from the cluster and use secret.data and
// secret.type to create a new secret in config-management-system namespace.
func update(ctx context.Context, existingsecret *corev1.Secret, namespaceSecret *corev1.Secret, c client.Client) error {
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
