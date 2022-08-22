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
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/util/watch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deleteWaitTimeout is the time to block for an object to be deleted.
// This should be long enough for foreground deletion of any one managed object,
// including garbage collection. The longest is usually the Deployment.
// However, even if the Deployment takes longer to delete, it's ok to timeout.
// Timeout will just cause an error, which causes reconcile to be retried by the
// controller-manager. Waiting is just an optimization to reduce API calls and
// error log messages, which could be red herrings.
const deleteWaitTimeout = 1 * time.Minute

// cleanup deletes the object and waits for it to be fully deleted.
func (r *reconcilerBase) cleanup(ctx context.Context, obj client.Object) error {
	err := watch.DeleteAndWait(ctx, r.watcher, obj, deleteWaitTimeout)
	if err != nil {
		return err
	}

	key := client.ObjectKeyFromObject(obj)
	gvk, err := kinds.Lookup(obj, r.scheme)
	if err != nil {
		return err
	}
	r.log.Info("Managed object delete confirmed",
		logFieldObject, key.String(),
		logFieldKind, gvk.Kind)
	return nil
}

func (r *RepoSyncReconciler) deleteSecrets(ctx context.Context, reconcilerRef types.NamespacedName) error {
	secretList := &corev1.SecretList{}
	if err := r.client.List(ctx, secretList, client.InNamespace(reconcilerRef.Namespace)); err != nil {
		return err
	}

	for _, s := range secretList.Items {
		if !strings.HasPrefix(s.Name, reconcilerRef.Name) {
			// Not for this reconciler
			continue
		}
		if err := r.cleanup(ctx, &s); err != nil {
			return err
		}
	}
	return nil
}

func (r *reconcilerBase) deleteConfigMaps(ctx context.Context, reconcilerRef types.NamespacedName) error {
	cmList := &corev1.ConfigMapList{}
	if err := r.client.List(ctx, cmList, client.InNamespace(reconcilerRef.Namespace)); err != nil {
		return err
	}

	cmNames := map[string]struct{}{
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.Reconciler):          {},
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.HydrationController): {},
		ReconcilerResourceName(reconcilerRef.Name, reconcilermanager.GitSync):             {},
	}

	for _, cm := range cmList.Items {
		if _, ok := cmNames[cm.Name]; !ok {
			// Not for this reconciler
			continue
		}
		if err := r.cleanup(ctx, &cm); err != nil {
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

// deleteRoleBinding deletes the reconciler ServiceAccount from the shared
// RoleBinding. The RoleBinding itself is deleted if all subjects are removed.
func (r *RepoSyncReconciler) deleteRoleBinding(ctx context.Context, rsNamespace string, reconcilerRef types.NamespacedName) error {
	rbKey := types.NamespacedName{Namespace: rsNamespace, Name: RepoSyncPermissionsName()}
	gvk := kinds.RoleBinding()
	subjectToDelete := subject(reconcilerRef.Name, reconcilerRef.Namespace, "ServiceAccount")

	rb := &rbacv1.RoleBinding{}
	if err := r.client.Get(ctx, rbKey, rb); err != nil {
		if apierrors.IsNotFound(err) {
			r.log.V(3).Info("Managed object already deleted",
				logFieldSubject, subjectToDelete,
				logFieldObject, rbKey.String(),
				logFieldKind, gvk.Kind)
			return nil
		}
		return errors.Wrapf(err, "failed to get the RoleBinding object %s", rbKey)
	}
	if !rb.DeletionTimestamp.IsZero() {
		r.log.V(3).Info("Managed object already terminating",
			logFieldObject, rbKey.String(),
			logFieldKind, gvk.Kind)
		// skip delete/update but still wait for delete
		return r.cleanup(ctx, rb)
	}
	if !r.deleteRoleBindingSubject(rb, subjectToDelete) {
		r.log.V(3).Info("Subject already removed from binding",
			logFieldSubject, subjectToDelete,
			logFieldObject, rbKey.String(),
			logFieldKind, gvk.Kind)
		return nil
	}
	if len(rb.Subjects) == 0 {
		return r.cleanup(ctx, rb)
	}
	err := r.client.Update(ctx, rb)
	if err != nil {
		r.log.Info("Subject successfully removed from binding",
			logFieldSubject, subjectToDelete,
			logFieldObject, rbKey.String(),
			logFieldKind, gvk.Kind)
	}
	return err
}

func (r *RepoSyncReconciler) deleteRoleBindingSubject(rb *rbacv1.RoleBinding, subjectToDelete rbacv1.Subject) bool {
	found := false
	for i, subject := range rb.Subjects {
		if subject == subjectToDelete {
			found = true
			rb.Subjects = append(rb.Subjects[:i], rb.Subjects[i+1:]...)
			break
		}
	}
	return found
}

// deleteClusterRoleBinding deletes the reconciler ServiceAccount from the
// shared ClusterRoleBinding. The ClusterRoleBinding itself is deleted if all
// subjects are removed.
func (r *RootSyncReconciler) deleteClusterRoleBinding(ctx context.Context, reconcilerRef types.NamespacedName) error {
	crbKey := client.ObjectKey{Name: RootSyncPermissionsName()}
	gvk := kinds.ClusterRoleBinding()
	subjectToDelete := subject(reconcilerRef.Name, reconcilerRef.Namespace, "ServiceAccount")

	crb := &rbacv1.ClusterRoleBinding{}
	if err := r.client.Get(ctx, crbKey, crb); err != nil {
		if apierrors.IsNotFound(err) {
			r.log.V(3).Info("Managed object already deleted",
				logFieldSubject, subjectToDelete,
				logFieldObject, crbKey.String(),
				logFieldKind, gvk.Kind)
			return nil
		}
		return errors.Wrapf(err, "failed to get the ClusterRoleBinding object %s", crbKey)
	}
	if !crb.DeletionTimestamp.IsZero() {
		r.log.V(3).Info("Managed object already terminating",
			logFieldObject, crbKey.String(),
			logFieldKind, gvk.Kind)
		// skip delete/update but still wait for delete
		return r.cleanup(ctx, crb)
	}
	if !r.deleteClusterRoleBindingSubject(crb, subjectToDelete) {
		r.log.V(3).Info("Subject already removed from binding",
			logFieldSubject, subjectToDelete,
			logFieldObject, crbKey.String(),
			logFieldKind, gvk.Kind)
		return nil
	}
	if len(crb.Subjects) == 0 {
		return r.cleanup(ctx, crb)
	}
	err := r.client.Update(ctx, crb)
	if err != nil {
		r.log.Info("Subject successfully removed from binding",
			logFieldSubject, subjectToDelete,
			logFieldObject, crbKey.String(),
			logFieldKind, gvk.Kind)
	}
	return err
}

func (r *RootSyncReconciler) deleteClusterRoleBindingSubject(crb *rbacv1.ClusterRoleBinding, subjectToDelete rbacv1.Subject) bool {
	found := false
	for i, subject := range crb.Subjects {
		if subject == subjectToDelete {
			found = true
			crb.Subjects = append(crb.Subjects[:i], crb.Subjects[i+1:]...)
			break
		}
	}
	return found
}

func (r *reconcilerBase) deleteDeployment(ctx context.Context, reconcilerRef types.NamespacedName) error {
	d := &appsv1.Deployment{}
	d.Name = reconcilerRef.Name
	d.Namespace = reconcilerRef.Namespace
	return r.cleanup(ctx, d)
}
