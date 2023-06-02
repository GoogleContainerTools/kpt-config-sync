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
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Cleanup namespace controller resources when reposync is not found since,
// we don't leverage ownerRef because namespace controller resources are
// created in config-management-system namespace instead of reposync namespace.
//
// NOTE: Update this method when resources created by namespace controller changes.
func (r *RepoSyncReconciler) cleanupNSControllerResources(ctx context.Context, rsKey, reconcilerRef types.NamespacedName) error {
	r.logger(ctx).Info("Deleting managed objects")

	// Delete namespace controller resources and return to reconcile loop in case
	// of errors to try cleaning up resources again.

	// Deployment
	if err := r.deleteDeployment(ctx, reconcilerRef); err != nil {
		return err
	}
	// configmaps
	if err := r.deleteConfigMaps(ctx, reconcilerRef); err != nil {
		return err
	}
	// serviceaccount
	if err := r.deleteServiceAccount(ctx, reconcilerRef); err != nil {
		return err
	}
	// rolebinding
	if err := r.deleteRoleBinding(ctx, reconcilerRef, rsKey); err != nil {
		return err
	}
	// secret
	return r.deleteSecrets(ctx, reconcilerRef)
}

func (r *reconcilerBase) cleanup(ctx context.Context, objRef types.NamespacedName, gvk schema.GroupVersionKind) error {
	u := &unstructured.Unstructured{}
	u.SetName(objRef.Name)
	u.SetNamespace(objRef.Namespace)
	u.SetGroupVersionKind(gvk)
	r.logger(ctx).Info("Deleting managed object",
		logFieldObjectRef, objRef.String(),
		logFieldObjectKind, gvk.Kind)
	if err := r.client.Delete(ctx, u); err != nil {
		if apierrors.IsNotFound(err) {
			r.logger(ctx).Info("Managed object already deleted",
				logFieldObjectRef, objRef.String(),
				logFieldObjectKind, gvk.Kind)
			return nil
		}
		return err
	}
	r.logger(ctx).Info("Managed object delete successful",
		logFieldObjectRef, objRef.String(),
		logFieldObjectKind, gvk.Kind)
	return nil
}

func (r *RepoSyncReconciler) deleteSecrets(ctx context.Context, reconcilerRef types.NamespacedName) error {
	secretList := &corev1.SecretList{}
	if err := r.client.List(ctx, secretList, client.InNamespace(reconcilerRef.Namespace)); err != nil {
		return err
	}

	for _, s := range secretList.Items {
		if strings.HasPrefix(s.Name, reconcilerRef.Name) {
			if err := r.cleanup(ctx, client.ObjectKeyFromObject(&s), kinds.Secret()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RepoSyncReconciler) deleteConfigMaps(ctx context.Context, reconcilerRef types.NamespacedName) error {
	cms := []string{
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.Reconciler),
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.HydrationController),
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.GitSync),
	}
	for _, name := range cms {
		key := types.NamespacedName{Namespace: reconcilerRef.Namespace, Name: name}
		if err := r.cleanup(ctx, key, kinds.ConfigMap()); err != nil {
			return err
		}
	}
	return nil
}

func (r *RepoSyncReconciler) deleteServiceAccount(ctx context.Context, reconcilerRef types.NamespacedName) error {
	return r.cleanup(ctx, reconcilerRef, kinds.ServiceAccount())
}

func (r *RepoSyncReconciler) deleteRoleBinding(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
	rbKey := client.ObjectKey{Namespace: rsRef.Namespace, Name: RepoSyncPermissionsName()}
	rb := &rbacv1.RoleBinding{}
	if err := r.client.Get(ctx, rbKey, rb); err != nil {
		if apierrors.IsNotFound(err) {
			// already deleted
			return nil
		}
		return errors.Wrapf(err, "failed to get the RoleBinding object %s", rbKey)
	}
	rb.Subjects = removeSubject(rb.Subjects, r.serviceAccountSubject(reconcilerRef))
	if len(rb.Subjects) == 0 {
		// Delete the whole RB
		return r.cleanup(ctx, rbKey, kinds.RoleBinding())
	}
	return r.client.Update(ctx, rb)
}

func (r *RepoSyncReconciler) deleteDeployment(ctx context.Context, reconcilerRef types.NamespacedName) error {
	return r.cleanup(ctx, reconcilerRef, kinds.Deployment())
}

func (r *RootSyncReconciler) deleteClusterRoleBinding(ctx context.Context, reconcilerRef types.NamespacedName) error {
	crbKey := client.ObjectKey{Name: RootSyncPermissionsName()}
	// Update the CRB to delete the subject for the deleted RootSync's reconciler
	crb := &rbacv1.ClusterRoleBinding{}
	if err := r.client.Get(ctx, crbKey, crb); err != nil {
		if apierrors.IsNotFound(err) {
			// already deleted
			return nil
		}
		return errors.Wrapf(err, "failed to get the ClusterRoleBinding object %s", crbKey)
	}
	crb.Subjects = removeSubject(crb.Subjects, r.serviceAccountSubject(reconcilerRef))
	if len(crb.Subjects) == 0 {
		// Delete the whole CRB
		return r.cleanup(ctx, crbKey, kinds.ClusterRoleBinding())
	}
	if err := r.client.Update(ctx, crb); err != nil {
		return errors.Wrapf(err, "failed to update the ClusterRoleBinding object %s", crbKey)
	}
	return nil
}
