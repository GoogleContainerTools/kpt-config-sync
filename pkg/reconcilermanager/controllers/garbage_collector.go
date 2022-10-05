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
	"sort"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
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
	r.log.Info("Deleting managed objects",
		logFieldObject, rsKey.String(),
		logFieldKind, r.syncKind)

	rsList := &v1beta1.RepoSyncList{}
	if err := r.client.List(ctx, rsList, client.InNamespace(rsKey.Namespace)); err != nil {
		return errors.Wrapf(err, "failed to list RepoSync managed objects in namespace %q", rsKey.Namespace)
	}

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
	if err := r.deleteRoleBinding(ctx, rsList, rsKey.Namespace, reconcilerRef.Namespace); err != nil {
		return err
	}
	// secret
	if err := r.deleteSecrets(ctx, reconcilerRef); err != nil {
		return err
	}

	delete(r.repoSyncs, rsKey)
	return nil
}

func (r *reconcilerBase) cleanup(ctx context.Context, key types.NamespacedName, gvk schema.GroupVersionKind) error {
	u := &unstructured.Unstructured{}
	u.SetName(key.Name)
	u.SetNamespace(key.Namespace)
	u.SetGroupVersionKind(gvk)
	if err := r.client.Delete(ctx, u); err != nil {
		if apierrors.IsNotFound(err) {
			r.log.Info("Managed object already deleted",
				logFieldObject, key.String(),
				logFieldKind, gvk.Kind)
			return nil
		}
		return err
	}
	r.log.Info("Managed object delete successful",
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

func (r *RepoSyncReconciler) deleteRoleBinding(ctx context.Context, rsList *v1beta1.RepoSyncList, rsNamespace, reconcilerNamespace string) error {
	rbName := RepoSyncPermissionsName()
	if len(rsList.Items) == 0 {
		return r.cleanup(ctx, client.ObjectKey{Namespace: rsNamespace, Name: rbName}, kinds.RoleBinding())
	}
	rb := &rbacv1.RoleBinding{}
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: rsNamespace, Name: rbName}, rb); err != nil {
		return errors.Wrapf(err, "failed to get the RoleBinding object %s/%s", rsNamespace, rbName)
	}
	if err := r.updateRoleBindingSubjects(rb, rsList, reconcilerNamespace); err != nil {
		return errors.Wrapf(err, "failed to update subjects of the RoleBinding object %s/%s", rsNamespace, rbName)
	}
	return r.client.Update(ctx, rb)
}

func (r *RepoSyncReconciler) deleteDeployment(ctx context.Context, reconcilerRef types.NamespacedName) error {
	return r.cleanup(ctx, reconcilerRef, kinds.Deployment())
}

func (r *RepoSyncReconciler) updateRoleBindingSubjects(rb *rbacv1.RoleBinding, rsList *v1beta1.RepoSyncList, reconcilerNamespace string) error {
	var subjects []rbacv1.Subject
	for _, rs := range rsList.Items {
		subjects = append(subjects, subject(core.NsReconcilerName(rs.Namespace, rs.Name),
			reconcilerNamespace,
			"ServiceAccount"))
	}
	sort.SliceStable(subjects, func(i, j int) bool {
		return subjects[i].Name < subjects[j].Name
	})
	rb.Subjects = subjects
	return nil
}

func (r *RootSyncReconciler) deleteClusterRoleBinding(ctx context.Context, reconcilerNamespace string) error {
	rootsyncList := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, rootsyncList, client.InNamespace(reconcilerNamespace)); err != nil {
		return errors.Wrapf(err, "failed to list the RootSync objects")
	}
	crbName := RootSyncPermissionsName()
	if len(rootsyncList.Items) == 0 {
		// Delete the whole CRB
		return r.cleanup(ctx, crbName, kinds.ClusterRoleBinding())
	}

	// Update the CRB to delete the subject for the deleted RootSync's reconciler
	crb := &rbacv1.ClusterRoleBinding{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: crbName}, crb); err != nil {
		return errors.Wrapf(err, "failed to get the ClusterRoleBinding object %s", crbName)
	}
	if err := r.updateClusterRoleBindingSubjects(crb, rootsyncList, reconcilerNamespace); err != nil {
		return errors.Wrapf(err, "failed to update the subjects of ClusterRoleBinding %s", crbName)
	}
	if err := r.client.Update(ctx, crb); err != nil {
		return errors.Wrapf(err, "failed to update the ClusterRoleBinding object %s", crbName)
	}
	return nil
}

// cleanup cleans up cluster-scoped resources that are created for RootSync.
// Other namespace-scoped resources are garbage collected via OwnerReferences.
// Cluster-scoped resources cannot be handled via OwnerReferences because
// RootSync is namespace-scoped, and cluster-scoped dependents can only specify
// cluster-scoped owners.
func (r *RootSyncReconciler) cleanup(ctx context.Context, name string, gvk schema.GroupVersionKind) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetGroupVersionKind(gvk)
	err := r.client.Delete(ctx, u)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.log.V(4).Info("cluster-scoped resource not present", "resource", name)
			return nil
		}
	}
	return err
}

func (r *RootSyncReconciler) updateClusterRoleBindingSubjects(crb *rbacv1.ClusterRoleBinding, rsList *v1beta1.RootSyncList, reconcilerNamespace string) error {
	var subjects []rbacv1.Subject
	for _, rs := range rsList.Items {
		subjects = append(subjects, subject(core.RootReconcilerName(rs.Name),
			reconcilerNamespace,
			"ServiceAccount"))
	}
	sort.SliceStable(subjects, func(i, j int) bool {
		return subjects[i].Name < subjects[j].Name
	})
	crb.Subjects = subjects
	return nil
}
