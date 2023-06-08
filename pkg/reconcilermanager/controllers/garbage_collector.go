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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *reconcilerBase) cleanup(ctx context.Context, obj client.Object) error {
	gvk, err := kinds.Lookup(obj, r.scheme)
	if err != nil {
		return err
	}
	id := core.ID{
		ObjectKey: client.ObjectKeyFromObject(obj),
		GroupKind: gvk.GroupKind(),
	}
	// Convert Object to Unstructured to avoid checking UID, ResourceVersion, or
	// modifying input object.
	u := &unstructured.Unstructured{}
	u.SetName(id.Name)
	u.SetNamespace(id.Namespace)
	u.SetGroupVersionKind(gvk)
	r.logger(ctx).Info("Deleting managed object",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind)
	if err := r.client.Delete(ctx, u); err != nil {
		if apierrors.IsNotFound(err) {
			r.logger(ctx).Info("Managed object already deleted",
				logFieldObjectRef, id.ObjectKey.String(),
				logFieldObjectKind, id.Kind)
			return nil
		}
		return NewObjectOperationErrorWithID(err, id, OperationDelete)
	}
	r.logger(ctx).Info("Managed object delete successful",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind)
	return nil
}

func (r *RepoSyncReconciler) deleteSecrets(ctx context.Context, reconcilerRef types.NamespacedName) error {
	secretList := &corev1.SecretList{}
	if err := r.client.List(ctx, secretList, client.InNamespace(reconcilerRef.Namespace)); err != nil {
		return NewObjectOperationErrorForListWithNamespace(err, secretList, OperationList, reconcilerRef.Namespace)
	}

	for _, s := range secretList.Items {
		if strings.HasPrefix(s.Name, reconcilerRef.Name) {
			if err := r.cleanup(ctx, &s); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *reconcilerBase) deleteConfigMaps(ctx context.Context, reconcilerRef types.NamespacedName) error {
	cms := []string{
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.Reconciler),
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.HydrationController),
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.GitSync),
	}
	for _, name := range cms {
		cm := &corev1.ConfigMap{}
		cm.Name = name
		cm.Namespace = reconcilerRef.Namespace
		if err := r.cleanup(ctx, cm); err != nil {
			return err
		}
	}
	return nil
}

func (r *reconcilerBase) deleteServiceAccount(ctx context.Context, reconcilerRef types.NamespacedName) error {
	sa := &corev1.ServiceAccount{}
	sa.Name = reconcilerRef.Name
	sa.Namespace = reconcilerRef.Namespace
	return r.cleanup(ctx, sa)
}

func (r *RepoSyncReconciler) deleteRoleBinding(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
	rbKey := client.ObjectKey{Namespace: rsRef.Namespace, Name: RepoSyncPermissionsName()}
	rb := &rbacv1.RoleBinding{}
	if err := r.client.Get(ctx, rbKey, rb); err != nil {
		if apierrors.IsNotFound(err) {
			// already deleted
			r.logger(ctx).Info("Managed object already deleted",
				logFieldObjectRef, rbKey.String(),
				logFieldObjectKind, "RoleBinding")
			return nil
		}
		return NewObjectOperationErrorWithKey(err, rb, OperationGet, rbKey)
	}
	count := len(rb.Subjects)
	rb.Subjects = removeSubject(rb.Subjects, r.serviceAccountSubject(reconcilerRef))
	if count == len(rb.Subjects) {
		// No change
		return nil
	}
	if len(rb.Subjects) == 0 {
		// Delete the whole RB
		return r.cleanup(ctx, rb)
	}
	if err := r.client.Update(ctx, rb); err != nil {
		return NewObjectOperationError(err, rb, OperationUpdate)
	}
	return nil
}

func (r *reconcilerBase) deleteDeployment(ctx context.Context, reconcilerRef types.NamespacedName) error {
	d := &appsv1.Deployment{}
	d.Name = reconcilerRef.Name
	d.Namespace = reconcilerRef.Namespace
	return r.cleanup(ctx, d)
}

func (r *RootSyncReconciler) deleteClusterRoleBinding(ctx context.Context, reconcilerRef types.NamespacedName) error {
	crbKey := client.ObjectKey{Name: RootSyncPermissionsName()}
	// Update the CRB to delete the subject for the deleted RootSync's reconciler
	crb := &rbacv1.ClusterRoleBinding{}
	if err := r.client.Get(ctx, crbKey, crb); err != nil {
		if apierrors.IsNotFound(err) {
			// already deleted
			r.logger(ctx).Info("Managed object already deleted",
				logFieldObjectRef, crbKey.String(),
				logFieldObjectKind, "ClusterRoleBinding")
			return nil
		}
		return NewObjectOperationErrorWithKey(err, crb, OperationGet, crbKey)
	}
	count := len(crb.Subjects)
	crb.Subjects = removeSubject(crb.Subjects, r.serviceAccountSubject(reconcilerRef))
	if count == len(crb.Subjects) {
		// No change
		return nil
	}
	if len(crb.Subjects) == 0 {
		// Delete the whole CRB
		return r.cleanup(ctx, crb)
	}
	if err := r.client.Update(ctx, crb); err != nil {
		return NewObjectOperationError(err, crb, OperationUpdate)
	}
	return nil
}
