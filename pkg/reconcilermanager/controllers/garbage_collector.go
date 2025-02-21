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
	"fmt"
	"slices"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/util/watch"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deleteWaitTimeout is the time to block watching for an object to be deleted.
//
// Shorter means the reconciler-manager will be more responsive.
// Longer means the reconciler-manager will be more efficient.
//
// Since reconcile timeout doesn't cause a Stalled condition, it's ok to timeout
// and wait for Reconcile to be triggered by another event.
const deleteWaitTimeout = 30 * time.Second

// cleanup deletes the object and waits for it to be fully deleted.
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
	uObj := &unstructured.Unstructured{}
	uObj.SetName(id.Name)
	uObj.SetNamespace(id.Namespace)
	uObj.SetGroupVersionKind(gvk)
	r.Logger(ctx).V(3).Info("Deleting managed object",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind)
	err = watch.DeleteAndWait(ctx, r.watcher, uObj, deleteWaitTimeout)
	if err != nil {
		// ErrWaitTimeout is returned when the context is Done, but doesn't
		// specify cancelled or deadline exceeded.
		if wait.Interrupted(err) {
			// If the DeleteAndWait observed any events, the object should have been updated.
			if uObj.GetDeletionTimestamp().IsZero() {
				// object not deleted
				return NewObjectOperationErrorWithID(err, id, OperationDelete)
			}
			result, computeErr := kstatus.Compute(uObj)
			if computeErr != nil {
				return fmt.Errorf("computing %s (%s) status failed: %w", id.Kind, id.ObjectKey, computeErr)
			}
			// Since DeletionTimestamp is set, this will be either Terminating or Failed
			return NewObjectReconcileErrorWithID(err, id, result.Status)
		}
		// If not a timeout, assume the delete failed.
		return NewObjectOperationErrorWithID(err, id, OperationDelete)
	}
	r.Logger(ctx).Info("Deleting managed object successful",
		logFieldObjectRef, id.ObjectKey.String(),
		logFieldObjectKind, id.Kind)
	return nil
}

func (r *RepoSyncReconciler) deleteSecrets(ctx context.Context, reconcilerRef types.NamespacedName, exceptions ...string) error {
	secretList := &corev1.SecretList{}
	if err := r.client.List(ctx, secretList, client.InNamespace(reconcilerRef.Namespace)); err != nil {
		return NewObjectOperationErrorForListWithNamespace(err, secretList, OperationList, reconcilerRef.Namespace)
	}

	for _, s := range secretList.Items {
		if slices.Contains(exceptions, s.Name) {
			// This Secret adheres to the current spec, skip deletion
			continue
		}
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
		key := client.ObjectKeyFromObject(cm)
		// Client cache is updated by watchers. So using Get to prevent delete
		// avoids unnecessary API calls.
		if err := r.client.Get(ctx, key, cm); err != nil {
			if apierrors.IsNotFound(err) {
				// Since these are obsolete, don't bother logging "already deleted"
				continue
			}
			return NewObjectOperationErrorWithKey(err, cm, OperationDelete, key)
		}
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

func (r *RepoSyncReconciler) deleteSharedRoleBinding(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
	rbKey := client.ObjectKey{Namespace: rsRef.Namespace, Name: RepoSyncBaseClusterRoleName}
	rb := &rbacv1.RoleBinding{}
	if err := r.client.Get(ctx, rbKey, rb); err != nil {
		if apierrors.IsNotFound(err) {
			// already deleted
			r.Logger(ctx).V(3).Info("Managed object already deleted",
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
	r.Logger(ctx).V(3).Info("Updating managed object",
		logFieldObjectRef, rbKey.String(),
		logFieldObjectKind, "RoleBinding")
	if err := r.client.Update(ctx, rb, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
		return NewObjectOperationError(err, rb, OperationUpdate)
	}
	r.Logger(ctx).Info("Updating managed object successful",
		logFieldObjectRef, rbKey.String(),
		logFieldObjectKind, "RoleBinding")
	return nil
}

// deleteHelmConfigMapCopies deletes helm values file ConfigMap copies,
// except those specified in the cmNamesToKeep set.
func (r *RepoSyncReconciler) deleteHelmConfigMapCopies(ctx context.Context, rsRef types.NamespacedName, cmNamesToKeep map[string]struct{}) error {
	// cleanup old copied ConfigMaps that we don't need anymore, e.g. if the list in valuesFileRefs changes
	// TODO: Eventually remove the annotations and use the labels for list filtering, to optimize cleanup.
	// We can't remove the annotations until v1.16.0 is no longer supported.
	var cmList corev1.ConfigMapList
	if err := r.client.List(ctx, &cmList, &client.ListOptions{Namespace: configsync.ControllerNamespace}); err != nil {
		return fmt.Errorf("config map list failed in namespace: %s: %w", configsync.ControllerNamespace, err)
	}
	for _, cm := range cmList.Items {
		if !strings.HasPrefix(cm.Name, core.NsReconcilerPrefix) {
			continue
		}
		labels := cm.GetLabels()
		if labels != nil {
			if value, found := labels[metadata.SyncKindLabel]; !found || value != configsync.RepoSyncKind {
				// this ConfigMap was not created for this RepoSync
				continue
			}
			if value, found := labels[metadata.SyncNameLabel]; !found || value != rsRef.Name {
				// this ConfigMap was not created for this RepoSync
				continue
			}
			if value, found := labels[metadata.SyncNamespaceLabel]; !found || value != rsRef.Namespace {
				// this ConfigMap was not created for this RepoSync
				continue
			}
		}
		annotations := cm.GetAnnotations()
		if annotations != nil {
			if value, found := annotations[repoSyncNameAnnotationKey]; !found || value != rsRef.Name {
				// this ConfigMap was not created for this RepoSync
				continue
			}
			if value, found := annotations[repoSyncNamespaceAnnotationKey]; !found || value != rsRef.Namespace {
				// this ConfigMap was not created for this RepoSync
				continue
			}
		}
		if cmNamesToKeep != nil {
			if _, found := cmNamesToKeep[cm.Name]; found {
				// this ConfigMap is still needed for this RepoSync
				continue
			}
		}
		// none of the above conditions apply, so this ConfigMap is no longer
		// needed, and we should delete it for cleanup
		if err := r.cleanup(ctx, &cm); err != nil {
			return err
		}
	}
	return nil
}

func (r *reconcilerBase) deleteDeployment(ctx context.Context, reconcilerRef types.NamespacedName) error {
	d := &appsv1.Deployment{}
	d.Name = reconcilerRef.Name
	d.Namespace = reconcilerRef.Namespace
	return r.cleanup(ctx, d)
}

func (r *reconcilerBase) deleteSharedClusterRoleBinding(ctx context.Context, name string, reconcilerRef types.NamespacedName) error {
	crbKey := client.ObjectKey{Name: name}
	// Update the CRB to delete the subject for the deleted reconciler
	crb := &rbacv1.ClusterRoleBinding{}
	if err := r.client.Get(ctx, crbKey, crb); err != nil {
		if apierrors.IsNotFound(err) {
			// already deleted
			r.Logger(ctx).V(3).Info("Managed object already deleted",
				logFieldObjectRef, crbKey.String(),
				logFieldObjectKind, "ClusterRoleBinding")
			return nil
		}
		return NewObjectOperationErrorWithKey(err, crb, OperationGet, crbKey)
	}
	count := len(crb.Subjects)
	crb.Subjects = removeSubject(crb.Subjects, r.serviceAccountSubject(reconcilerRef))
	if len(crb.Subjects) == 0 {
		// Delete the whole CRB
		return r.cleanup(ctx, crb)
	}
	if count == len(crb.Subjects) {
		// No change
		return nil
	}
	r.Logger(ctx).V(3).Info("Updating managed object",
		logFieldObjectRef, crbKey.String(),
		logFieldObjectKind, "ClusterRoleBinding")
	if err := r.client.Update(ctx, crb, client.FieldOwner(reconcilermanager.FieldManager)); err != nil {
		return NewObjectOperationError(err, crb, OperationUpdate)
	}
	r.Logger(ctx).Info("Updating managed object successful",
		logFieldObjectRef, crbKey.String(),
		logFieldObjectKind, "ClusterRoleBinding")
	return nil
}
