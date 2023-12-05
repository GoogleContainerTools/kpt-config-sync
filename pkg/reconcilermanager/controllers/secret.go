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
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// isUpsertedSecret returns true if the provided secret from the
// config-management-system namespace was upserted by the Reconciler
func isUpsertedSecret(rs *v1beta1.RepoSync, secretName string) bool {
	reconcilerName := core.NsReconcilerName(rs.GetNamespace(), rs.GetName())
	if shouldUpsertCACertSecret(rs) && secretName == ReconcilerResourceName(reconcilerName, v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef)) {
		return true
	}
	if shouldUpsertGitSecret(rs) && secretName == ReconcilerResourceName(reconcilerName, v1beta1.GetSecretName(rs.Spec.Git.SecretRef)) {
		return true
	}
	if shouldUpsertHelmSecret(rs) && secretName == ReconcilerResourceName(reconcilerName, v1beta1.GetSecretName(rs.Spec.Helm.SecretRef)) {
		return true
	}
	return false
}

func shouldUpsertCACertSecret(rs *v1beta1.RepoSync) bool {
	return v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.GitSource && rs.Spec.Git != nil && rs.Spec.CACertSecretRef != nil && useCACert(rs.Spec.CACertSecretRef.Name)
}

func shouldUpsertGitSecret(rs *v1beta1.RepoSync) bool {
	return v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.GitSource && rs.Spec.Git != nil && rs.Spec.Git.SecretRef != nil && !SkipForAuth(rs.Spec.Auth)
}

func shouldUpsertHelmSecret(rs *v1beta1.RepoSync) bool {
	return v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.HelmSource && rs.Spec.Helm != nil && rs.Spec.Helm.SecretRef != nil && !SkipForAuth(rs.Spec.Helm.Auth)
}

// upsertAuthSecret creates or updates the auth secret in the
// config-management-system namespace using an existing secret in the RepoSync
// namespace.
func (r *reconcilerBase) upsertAuthSecret(ctx context.Context, rs *v1beta1.RepoSync, reconcilerRef types.NamespacedName, labelMap map[string]string) (client.ObjectKey, error) {
	rsRef := client.ObjectKeyFromObject(rs)
	switch {
	case shouldUpsertGitSecret(rs):
		nsSecretRef, cmsSecretRef := getSecretRefs(rsRef, reconcilerRef, v1beta1.GetSecretName(rs.Spec.Git.SecretRef))
		userSecret, err := getUserSecret(ctx, r.client, nsSecretRef)
		if err != nil {
			return cmsSecretRef, errors.Wrap(err, "user secret required for git client authentication")
		}
		_, err = r.upsertSecret(ctx, cmsSecretRef, userSecret, labelMap)
		return cmsSecretRef, err
	case shouldUpsertHelmSecret(rs):
		nsSecretRef, cmsSecretRef := getSecretRefs(rsRef, reconcilerRef, v1beta1.GetSecretName(rs.Spec.Helm.SecretRef))
		userSecret, err := getUserSecret(ctx, r.client, nsSecretRef)
		if err != nil {
			return cmsSecretRef, errors.Wrap(err, "user secret required for helm client authentication")
		}
		_, err = r.upsertSecret(ctx, cmsSecretRef, userSecret, labelMap)
		return cmsSecretRef, err
	default:
		// No secret required
		return client.ObjectKey{}, nil
	}
}

// upsertCACertSecret creates or updates the CA cert secret in the
// config-management-system namespace using an existing secret in the RepoSync
// namespace.
func (r *reconcilerBase) upsertCACertSecret(ctx context.Context, rs *v1beta1.RepoSync, reconcilerRef types.NamespacedName, labelMap map[string]string) (client.ObjectKey, error) {
	rsRef := client.ObjectKeyFromObject(rs)
	if shouldUpsertCACertSecret(rs) {
		nsSecretRef, cmsSecretRef := getSecretRefs(rsRef, reconcilerRef, v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef))
		userSecret, err := getUserSecret(ctx, r.client, nsSecretRef)
		if err != nil {
			return cmsSecretRef, errors.Wrap(err, "user secret required for git server validation")
		}
		_, err = r.upsertSecret(ctx, cmsSecretRef, userSecret, labelMap)
		return cmsSecretRef, err
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
func (r *reconcilerBase) upsertSecret(ctx context.Context, cmsSecretRef types.NamespacedName, userSecret *corev1.Secret, labelMap map[string]string) (controllerutil.OperationResult, error) {
	cmsSecret := &corev1.Secret{}
	cmsSecret.Name = cmsSecretRef.Name
	cmsSecret.Namespace = cmsSecretRef.Namespace

	op, err := CreateOrUpdate(ctx, r.client, cmsSecret, func() error {
		core.AddLabels(cmsSecret, labelMap)
		// Copy user secret data & type to managed secret
		cmsSecret.Data = userSecret.Data
		cmsSecret.Type = userSecret.Type
		return nil
	})
	if err != nil {
		return op, err
	}
	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, cmsSecretRef.String(),
			logFieldObjectKind, "Secret",
			logFieldOperation, op)
	}
	return op, nil
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

// SkipForAuth returns true if the passed auth is either 'none' or 'gcenode', 'gcpksa' or
// 'gcpserviceaccount'.
func SkipForAuth(auth configsync.AuthType) bool {
	switch auth {
	case configsync.AuthNone, configsync.AuthGCENode, configsync.AuthGCPServiceAccount, configsync.AuthGCPKSA:
		return true
	default:
		return false
	}
}
