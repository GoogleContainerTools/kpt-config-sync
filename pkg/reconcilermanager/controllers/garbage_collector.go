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
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconciler"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Cleanup namespace controller resources when reposync is not found since,
// we don't leverage ownerRef because namespace controller resources are
// created in config-management-system namespace instead of reposync namespace.
//
// NOTE: Update this method when resources created by namespace controller changes.
func (r *RepoSyncReconciler) cleanupNSControllerResources(ctx context.Context, ns, rsName string) error {
	reconcilerName := reconciler.NsReconcilerName(ns, rsName)
	r.log.Info("Cleaning up namespace controller resources", "reconcilerName", reconcilerName)

	reposyncList := &v1beta1.RepoSyncList{}
	if err := r.client.List(ctx, reposyncList, client.InNamespace(ns)); err != nil {
		return errors.Wrapf(err, "failed to list the RepoSync objects in namespace %q", ns)
	}

	// Delete namespace controller resources and return to reconcile loop in case
	// of errors to try cleaning up resources again.

	// Deployment
	if err := r.deleteDeployment(ctx, reconcilerName); err != nil {
		return err
	}
	// configmaps
	if err := r.deleteConfigmap(ctx, reconcilerName); err != nil {
		return err
	}
	// serviceaccount
	if err := r.deleteServiceAccount(ctx, reconcilerName); err != nil {
		return err
	}
	// rolebinding
	if err := r.deleteRoleBinding(ctx, ns, reposyncList); err != nil {
		return err
	}
	// secret
	if err := r.deleteSecret(ctx, reconcilerName); err != nil {
		return err
	}

	delete(r.repoSyncs, types.NamespacedName{Namespace: ns, Name: rsName})
	return nil
}

func (r *RepoSyncReconciler) cleanup(ctx context.Context, name, namespace string, gvk schema.GroupVersionKind) error {
	u := &unstructured.Unstructured{}
	u.SetName(name)
	u.SetNamespace(namespace)
	u.SetGroupVersionKind(gvk)
	err := r.client.Delete(ctx, u)
	if err != nil {
		if apierrors.IsNotFound(err) {
			r.log.V(4).Info("resource not present", "namespace", namespace, "resource", name)
			return nil
		}
	}
	return err
}

func (r *RepoSyncReconciler) deleteSecret(ctx context.Context, reconcilerName string) error {
	secretList := &corev1.SecretList{}
	if err := r.client.List(ctx, secretList, client.InNamespace(configsync.ControllerNamespace)); err != nil {
		return err
	}

	for _, s := range secretList.Items {
		if strings.HasPrefix(s.Name, reconcilerName) {
			if err := r.cleanup(ctx, s.Name, s.Namespace, kinds.Secret()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *RepoSyncReconciler) deleteConfigmap(ctx context.Context, reconcilerName string) error {
	cms := []string{
		ReconcilerResourceName(reconcilerName, reconcilermanager.Reconciler),
		ReconcilerResourceName(reconcilerName, reconcilermanager.HydrationController),
		ReconcilerResourceName(reconcilerName, reconcilermanager.GitSync),
	}
	for _, c := range cms {
		if err := r.cleanup(ctx, c, v1.NSConfigManagementSystem, kinds.ConfigMap()); err != nil {
			return err
		}
	}
	return nil
}

func (r *RepoSyncReconciler) deleteServiceAccount(ctx context.Context, reconcilerName string) error {
	return r.cleanup(ctx, reconcilerName, v1.NSConfigManagementSystem, kinds.ServiceAccount())
}

func (r *RepoSyncReconciler) deleteRoleBinding(ctx context.Context, namespace string, reposyncList *v1beta1.RepoSyncList) error {
	rbName := RepoSyncPermissionsName()
	if len(reposyncList.Items) == 0 {
		return r.cleanup(ctx, rbName, namespace, kinds.RoleBinding())
	}
	rb := &rbacv1.RoleBinding{}
	if err := r.client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: rbName}, rb); err != nil {
		return errors.Wrapf(err, "failed to get the RoleBinding object %s/%s", namespace, rbName)
	}
	if err := r.updateRoleBindingSubjects(rb, reposyncList); err != nil {
		return errors.Wrapf(err, "failed to update subjects of the RoleBinding object %s/%s", namespace, rbName)
	}
	return r.client.Update(ctx, rb)
}

func (r *RepoSyncReconciler) deleteDeployment(ctx context.Context, reconcilerName string) error {
	return r.cleanup(ctx, reconcilerName, v1.NSConfigManagementSystem, kinds.Deployment())
}

func (r *RepoSyncReconciler) updateRoleBindingSubjects(rb *rbacv1.RoleBinding, rsList *v1beta1.RepoSyncList) error {
	var subjects []rbacv1.Subject
	for _, rs := range rsList.Items {
		subjects = append(subjects, subject(reconciler.NsReconcilerName(rs.Namespace, rs.Name),
			configsync.ControllerNamespace,
			"ServiceAccount"))
	}
	sort.SliceStable(subjects, func(i, j int) bool {
		return subjects[i].Name < subjects[j].Name
	})
	rb.Subjects = subjects
	return nil
}

func (r *RootSyncReconciler) deleteClusterRoleBinding(ctx context.Context) error {
	rootsyncList := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, rootsyncList, client.InNamespace(configsync.ControllerNamespace)); err != nil {
		return errors.Wrapf(err, "failed to list the RootSync objects")
	}
	crbName := RootSyncPermissionsName()
	if len(rootsyncList.Items) == 0 {
		return r.cleanup(ctx, crbName, kinds.ClusterRoleBinding())
	}
	crb := &rbacv1.ClusterRoleBinding{}
	if err := r.client.Get(ctx, client.ObjectKey{Name: crbName}, crb); err != nil {
		return errors.Wrapf(err, "failed to get the ClusterRoleBinding object %s", crbName)
	}
	return r.updateClusterRoleBindingSubjects(crb, rootsyncList)
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

func (r *RootSyncReconciler) updateClusterRoleBindingSubjects(crb *rbacv1.ClusterRoleBinding, rsList *v1beta1.RootSyncList) error {
	var subjects []rbacv1.Subject
	for _, rs := range rsList.Items {
		subjects = append(subjects, subject(reconciler.RootReconcilerName(rs.Name),
			configsync.ControllerNamespace,
			"ServiceAccount"))
	}
	sort.SliceStable(subjects, func(i, j int) bool {
		return subjects[i].Name < subjects[j].Name
	})
	crb.Subjects = subjects
	return nil
}
