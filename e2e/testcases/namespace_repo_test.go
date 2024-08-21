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

package e2e

import (
	"fmt"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"kpt.dev/configsync/e2e/nomostest"
	"kpt.dev/configsync/e2e/nomostest/gitproviders"
	"kpt.dev/configsync/e2e/nomostest/metrics"
	"kpt.dev/configsync/e2e/nomostest/ntopts"
	"kpt.dev/configsync/e2e/nomostest/policy"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	nomostesting "kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/applier"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/core/k8sobjects"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNamespaceRepo_Centralized(t *testing.T) {
	bsNamespace := "bookstore"
	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
		ntopts.WithCentralizedControl,
	)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, bsNamespace)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitRepository(repoSyncID)

	// Validate status condition "Reconciling" and "Stalled "is set to "False"
	// after the reconciler deployment is successfully created.
	// RepoSync status conditions "Reconciling" and "Stalled" are derived from
	// namespace reconciler deployment.
	// Log error if the Reconciling condition does not progress to False before
	// the timeout expires.
	err := nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), "repo-sync", bsNamespace,
		[]testpredicates.Predicate{
			hasReconcilingStatus(metav1.ConditionFalse),
			hasStalledStatus(metav1.ConditionFalse),
		},
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Errorf("RepoSync did not finish reconciling: %v", err)
	}

	// Validate service account 'store' not present.
	err = nt.ValidateNotFound("store", bsNamespace, &corev1.ServiceAccount{})
	if err != nil {
		nt.T.Errorf("store service account already present: %v", err)
	}

	sa := k8sobjects.ServiceAccountObject("store", core.Namespace(bsNamespace))
	nt.Must(repoSyncGitRepo.Add("acme/sa.yaml", sa))
	nt.Must(repoSyncGitRepo.CommitAndPush("Adding service account"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Validate service account 'store' is current.
	err = nt.Watcher.WatchForCurrentStatus(kinds.ServiceAccount(), "store", bsNamespace,
		testwatcher.WatchTimeout(30*time.Second))
	if err != nil {
		nt.T.Fatalf("service account store not found: %v", err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncKey, sa)

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	validateRepoSyncRBAC(nt, bsNamespace, repoSyncGitRepo, configureRBACInCentralizedMode)
}

func validateRepoSyncRBAC(nt *nomostest.NT, ns string, nsRepo *gitproviders.Repository, configureRBAC configureRBACFunc) {
	nt.T.Cleanup(func() {
		// Grant full permission to manage the Deployment in case the test fails early
		configureRBAC(nt, ns, []string{"*"})
	})
	nt.T.Log("Add a deployment to the namespace repo without RBAC")
	nt.Must(nsRepo.Copy(fmt.Sprintf("%s/deployment-helloworld.yaml", yamlDir), "acme/deployment.yaml"))
	nt.Must(nsRepo.CommitAndPush("Add a deployment to the namespace repo"))
	nt.WaitForRepoSyncSyncError(ns, configsync.RepoSyncName, applier.ApplierErrorCode,
		`polling for status failed: failed to list apps/v1, Kind=Deployment: deployments.apps is forbidden: User "system:serviceaccount:config-management-system:ns-reconciler-bookstore" cannot list resource "deployments" in API group "apps" in the namespace "bookstore"`, nil)

	nt.T.Log("Add 'list' permission")
	configureRBAC(nt, ns, []string{"list"})
	nt.WaitForRepoSyncSyncError(ns, configsync.RepoSyncName, applier.ApplierErrorCode,
		`polling for status failed: unknown`, nil)

	// The unknown error is caused by missing `watch` permission and should be fixed upstream with more details.
	nt.T.Log("Add 'watch' permission")
	configureRBAC(nt, ns, []string{"list", "watch"})
	nt.WaitForRepoSyncSyncError(ns, configsync.RepoSyncName, applier.ApplierErrorCode,
		`failed to apply Deployment.apps, bookstore/hello-world: failed to get current object from cluster: deployments.apps "hello-world" is forbidden: User "system:serviceaccount:config-management-system:ns-reconciler-bookstore" cannot get resource "deployments" in API group "apps" in the namespace "bookstore"`+"\n\nsource: acme/deployment.yaml", []v1beta1.ResourceRef{{
			SourcePath: "acme/deployment.yaml",
			Name:       "hello-world",
			Namespace:  "bookstore",
			GVK: metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
		}})

	nt.T.Log("Add 'get' permission")
	configureRBAC(nt, ns, []string{"list", "watch", "get"})
	nt.WaitForRepoSyncSyncError(ns, configsync.RepoSyncName, applier.ApplierErrorCode,
		`failed to apply Deployment.apps, bookstore/hello-world: deployments.apps "hello-world" is forbidden: User "system:serviceaccount:config-management-system:ns-reconciler-bookstore" cannot patch resource "deployments" in API group "apps" in the namespace "bookstore"`, []v1beta1.ResourceRef{{
			SourcePath: "acme/deployment.yaml",
			Name:       "hello-world",
			Namespace:  "bookstore",
			GVK: metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
		}})

	nt.T.Log("Add 'patch' permission")
	configureRBAC(nt, ns, []string{"list", "watch", "get", "patch"})
	nt.T.Log("Restart the namespace reconciler Pod to force resync instead of waiting for the retry backoff")
	// Prior updates don't need a restart because the retry backoff is within 1 minute
	// A Pod restart on Autopilot may take longer than 1 minute
	nomostest.DeletePodByLabel(nt, "configsync.gke.io/deployment-name", core.NsReconcilerName(ns, configsync.RepoSyncName), false)
	nt.WaitForRepoSyncSyncError(ns, configsync.RepoSyncName, applier.ApplierErrorCode,
		`failed to apply Deployment.apps, bookstore/hello-world: deployments.apps "hello-world" is forbidden: `, []v1beta1.ResourceRef{{
			SourcePath: "acme/deployment.yaml",
			Name:       "hello-world",
			Namespace:  "bookstore",
			GVK: metav1.GroupVersionKind{
				Group:   "apps",
				Version: "v1",
				Kind:    "Deployment",
			},
		}})
	nt.T.Log("Add 'create' permission")
	configureRBAC(nt, ns, []string{"list", "watch", "get", "patch", "create"})
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.Validate("hello-world", ns, &appsv1.Deployment{}); err != nil {
		nt.T.Fatalf("deployment hello-world not found: %v", err)
	}

	nt.Must(nsRepo.Remove("acme/deployment.yaml"))
	nt.Must(nsRepo.CommitAndPush("Removing Deployment"))
	nt.WaitForRepoSyncSyncError(ns, configsync.RepoSyncName, applier.ApplierErrorCode,
		`failed to prune Deployment.apps, bookstore/hello-world: deployments.apps "hello-world" is forbidden: User "system:serviceaccount:config-management-system:ns-reconciler-bookstore" cannot delete resource "deployments" in API group "apps" in the namespace "bookstore"`, nil)
	nt.T.Log("Add 'delete' permission")
	configureRBAC(nt, ns, []string{"list", "watch", "get", "patch", "create", "delete"})
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.ValidateNotFound("hello-world", ns, &appsv1.Deployment{}); err != nil {
		nt.T.Fatal(err)
	}
}

type configureRBACFunc func(nt *nomostest.NT, ns string, verbs []string)

func configureRBACInCentralizedMode(nt *nomostest.NT, ns string, verbs []string) {
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{appsv1.GroupName},
			Resources: []string{"deployments"},
			Verbs:     verbs,
		},
	}
	rsRole := k8sobjects.RoleObject(
		core.Name("deployment-admin"),
		core.Namespace(ns),
	)
	rsRole.Rules = rules
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(ns, fmt.Sprintf("role-%s", rsRole.Name)), rsRole))
	rsRoleRef := rbacv1.RoleRef{
		APIGroup: rsRole.GroupVersionKind().Group,
		Kind:     rsRole.Kind,
		Name:     rsRole.Name,
	}
	rb := k8sobjects.RoleBindingObject(core.Name("syncs-repo"), core.Namespace(ns))
	sb := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      core.NsReconcilerName(ns, configsync.RepoSyncName),
			Namespace: configmanagement.ControllerNamespace,
		},
	}
	rb.Subjects = sb
	rb.RoleRef = rsRoleRef
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(ns, fmt.Sprintf("rb-%s", rb.Name)), rb))
	nt.Must(rootSyncGitRepo.CommitAndPush("Adding restricted role and rolebinding for RepoSync"))
}

func configureRBACInDelegatedMode(nt *nomostest.NT, ns string, verbs []string) {
	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{appsv1.GroupName},
			Resources: []string{"deployments"},
			Verbs:     verbs,
		},
	}
	rsRole := k8sobjects.RoleObject(
		core.Name("deployment-admin"),
		core.Namespace(ns),
	)
	rsRole.Rules = rules
	if err := nt.KubeClient.Apply(rsRole); err != nil {
		nt.T.Fatal(err)
	}
	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Get(rsRole.Name, rsRole.Namespace, &rbacv1.Role{}); err == nil {
			if err := nt.KubeClient.Delete(rsRole); err != nil {
				nt.T.Error(err)
			}
		} else if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}

	})

	rsRoleRef := rbacv1.RoleRef{
		APIGroup: rsRole.GroupVersionKind().Group,
		Kind:     rsRole.Kind,
		Name:     rsRole.Name,
	}
	rb := k8sobjects.RoleBindingObject(core.Name("syncs-repo"), core.Namespace(ns))
	sb := []rbacv1.Subject{
		{
			Kind:      "ServiceAccount",
			Name:      core.NsReconcilerName(ns, configsync.RepoSyncName),
			Namespace: configmanagement.ControllerNamespace,
		},
	}
	rb.Subjects = sb
	rb.RoleRef = rsRoleRef

	if err := nt.KubeClient.Apply(rb); err != nil {
		nt.T.Fatal(err)

	}
	nt.T.Cleanup(func() {
		if err := nt.KubeClient.Get(rb.Name, rb.Namespace, &rbacv1.RoleBinding{}); err == nil {
			if err = nt.KubeClient.Delete(rb); err != nil {
				nt.T.Error(err)
			}
		} else if !apierrors.IsNotFound(err) {
			nt.T.Error(err)
		}
	})
}

func hasReconcilingStatus(r metav1.ConditionStatus) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs := o.(*v1beta1.RepoSync)
		conditions := rs.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == v1beta1.RepoSyncReconciling && condition.Status != r {
				return fmt.Errorf("object %q has %q condition status %q; want %q", o.GetName(), condition.Type, string(condition.Status), r)
			}
		}
		return nil
	}
}

func hasStalledStatus(r metav1.ConditionStatus) testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		rs := o.(*v1beta1.RepoSync)
		conditions := rs.Status.Conditions
		for _, condition := range conditions {
			if condition.Type == v1beta1.RepoSyncStalled && condition.Status != r {
				return fmt.Errorf("object %q has %q condition status %q; want %q", o.GetName(), condition.Type, string(condition.Status), r)
			}
		}
		return nil
	}
}

func TestNamespaceRepo_Delegated(t *testing.T) {
	bsNamespaceRepo := "bookstore"
	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespaceRepo, configsync.RepoSyncName),
		ntopts.WithDelegatedControl,
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
	)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, bsNamespaceRepo)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitRepository(repoSyncID)

	// Validate service account 'store' not present.
	err := nt.ValidateNotFound("store", bsNamespaceRepo, &corev1.ServiceAccount{})
	if err != nil {
		nt.T.Errorf("store service account already present: %v", err)
	}

	sa := k8sobjects.ServiceAccountObject("store", core.Namespace(bsNamespaceRepo))
	nt.Must(repoSyncGitRepo.Add("acme/sa.yaml", sa))
	nt.Must(repoSyncGitRepo.CommitAndPush("Adding service account"))
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	// Validate service account 'store' is present.
	err = nt.Validate("store", bsNamespaceRepo, &corev1.ServiceAccount{})
	if err != nil {
		nt.T.Error(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RepoSyncKind, repoSyncKey, sa)

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	validateRepoSyncRBAC(nt, bsNamespaceRepo, repoSyncGitRepo, configureRBACInDelegatedMode)
}

func TestDeleteRepoSync_Delegated_AndRepoSyncV1Alpha1(t *testing.T) {
	bsNamespace := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithDelegatedControl,
	)

	var rs v1beta1.RepoSync
	if err := nt.KubeClient.Get(configsync.RepoSyncName, bsNamespace, &rs); err != nil {
		nt.T.Fatal(err)
	}
	secretNames := getNsReconcilerSecrets(nt, bsNamespace)

	if err := nomostest.DeleteObjectsAndWait(nt, &rs); err != nil {
		nt.T.Fatal(err)
	}

	checkRepoSyncResourcesNotPresent(nt, bsNamespace, secretNames)

	nt.T.Log("Test RepoSync v1alpha1 version in delegated control mode")
	nn := nomostest.RepoSyncNN(bsNamespace, configsync.RepoSyncName)
	rsv1alpha1 := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, nn)
	if err := nt.KubeClient.Create(rsv1alpha1); err != nil {
		nt.T.Fatal(err)
	}
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestDeleteRepoSync_Centralized_AndRepoSyncV1Alpha1(t *testing.T) {
	rootSyncNN := nomostest.RootSyncNN(configsync.RootSyncName)
	bsNamespace := "bookstore"

	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithCentralizedControl,
	)
	rootSyncGitRepo := nt.SyncSourceGitRepository(nomostest.DefaultRootSyncID)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, bsNamespace)
	repoSyncKey := repoSyncID.ObjectKey

	secretNames := getNsReconcilerSecrets(nt, bsNamespace)

	repoSyncPath := nomostest.StructuredNSPath(bsNamespace, configsync.RepoSyncName)
	repoSyncObj := rootSyncGitRepo.MustGet(nt.T, repoSyncPath)

	// Remove RepoSync resource from Root Repository.
	nt.Must(rootSyncGitRepo.Remove(repoSyncPath))
	nt.Must(rootSyncGitRepo.CommitAndPush("Removing RepoSync from the Root Repository"))

	// Remove from NamespaceRepos so we don't try to check that it is syncing,
	// as we've just deleted it.
	repoSyncSource, found := nt.SyncSources[repoSyncID]
	if !found {
		nt.T.Fatalf("Missing %s: %s", repoSyncID.Kind, repoSyncID.ObjectKey)
	}
	delete(nt.SyncSources, repoSyncID)
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	checkRepoSyncResourcesNotPresent(nt, bsNamespace, secretNames)

	nt.MetricsExpectations.AddObjectDelete(configsync.RootSyncKind, rootSyncNN, repoSyncObj)

	err := nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	nt.T.Log("Test RepoSync v1alpha1 version in central control mode")
	nt.SyncSources[repoSyncID] = repoSyncSource
	rs := nomostest.RepoSyncObjectV1Alpha1FromNonRootRepo(nt, repoSyncKey)
	nt.Must(rootSyncGitRepo.Add(nomostest.StructuredNSPath(bsNamespace, rs.Name), rs))
	nt.Must(rootSyncGitRepo.CommitAndPush("Add RepoSync v1alpha1"))
	// Add the bookstore namespace repo back to NamespaceRepos to verify that it is synced.
	if err := nt.WatchForAllSyncs(); err != nil {
		nt.T.Fatal(err)
	}

	nt.MetricsExpectations.AddObjectApply(configsync.RootSyncKind, rootSyncNN, rs)

	err = nomostest.ValidateStandardMetricsForRootSync(nt, metrics.Summary{
		Sync: rootSyncNN,
	})
	if err != nil {
		nt.T.Fatal(err)
	}

	err = nomostest.ValidateStandardMetricsForRepoSync(nt, metrics.Summary{
		Sync: repoSyncKey,
	})
	if err != nil {
		nt.T.Fatal(err)
	}
}

func TestManageSelfRepoSync(t *testing.T) {
	bsNamespace := "bookstore"
	nt := nomostest.New(t, nomostesting.MultiRepos,
		ntopts.RepoSyncPermissions(policy.CoreAdmin()), // NS Reconciler manages ServiceAccounts
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName))
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, bsNamespace)
	repoSyncGitRepo := nt.SyncSourceGitRepository(repoSyncID)

	rs := &v1beta1.RepoSync{}
	if err := nt.KubeClient.Get(repoSyncID.Name, repoSyncID.Namespace, rs); err != nil {
		nt.T.Fatal(err)
	}
	sanitizedRs := k8sobjects.RepoSyncObjectV1Beta1(rs.Namespace, rs.Name)
	sanitizedRs.Spec = rs.Spec
	nt.Must(repoSyncGitRepo.Add("acme/repo-sync.yaml", sanitizedRs))
	nt.Must(repoSyncGitRepo.CommitAndPush("add the repo-sync object that configures the reconciler"))
	nt.WaitForRepoSyncSourceError(rs.Namespace, rs.Name, validate.SelfReconcileErrorCode, "RepoSync bookstore/repo-sync must not manage itself in its repo")
}

func getNsReconcilerSecrets(nt *nomostest.NT, ns string) []string {
	secretList := &corev1.SecretList{}
	if err := nt.KubeClient.List(secretList, client.InNamespace(configsync.ControllerNamespace)); err != nil {
		nt.T.Fatal(err)
	}
	var secretNames []string
	for _, secret := range secretList.Items {
		if strings.HasPrefix(secret.Name, core.NsReconcilerName(ns, configsync.RepoSyncName)) {
			secretNames = append(secretNames, secret.Name)
		}
	}
	return secretNames
}

func checkRepoSyncResourcesNotPresent(nt *nomostest.NT, namespace string, secretNames []string) {
	tg := taskgroup.New()
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.RepoSyncV1Beta1(), configsync.RepoSyncName, namespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.Deployment(), core.NsReconcilerName(namespace, configsync.RepoSyncName), configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.ConfigMap(), "ns-reconciler-bookstore-git-sync", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.ConfigMap(), "ns-reconciler-bookstore-reconciler", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.ConfigMap(), "ns-reconciler-bookstore-hydration-controller", configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.ServiceAccount(), core.NsReconcilerName(namespace, configsync.RepoSyncName), configsync.ControllerNamespace)
	})
	tg.Go(func() error {
		return nt.Watcher.WatchForNotFound(kinds.ServiceAccount(), controllers.RepoSyncBaseClusterRoleName, configsync.ControllerNamespace)
	})
	for _, sName := range secretNames {
		nn := types.NamespacedName{Name: sName, Namespace: configsync.ControllerNamespace}
		tg.Go(func() error {
			return nt.Watcher.WatchForNotFound(kinds.Secret(), nn.Name, nn.Namespace)
		})
	}
	if err := tg.Wait(); err != nil {
		nt.T.Fatal(err)
	}
}

func TestDeleteNamespaceReconcilerDeployment(t *testing.T) {
	bsNamespace := "bookstore"
	nt := nomostest.New(
		t,
		nomostesting.MultiRepos,
		ntopts.NamespaceRepo(bsNamespace, configsync.RepoSyncName),
		ntopts.WithCentralizedControl,
	)
	rootSyncID := nomostest.DefaultRootSyncID
	rootSyncKey := rootSyncID.ObjectKey
	rootSyncGitRepo := nt.SyncSourceGitRepository(rootSyncID)
	repoSyncID := nomostest.RepoSyncID(configsync.RepoSyncName, bsNamespace)
	repoSyncKey := repoSyncID.ObjectKey
	repoSyncGitRepo := nt.SyncSourceGitRepository(repoSyncID)

	nsReconciler := core.NsReconcilerName(repoSyncID.Namespace, repoSyncID.Name)

	// Validate status condition "Reconciling" and Stalled is set to "False" after
	// the reconciler deployment is successfully created.
	// RepoSync status conditions "Reconciling" and "Stalled" are derived from
	// namespace reconciler deployment.
	// Retry before checking for Reconciling and Stalled conditions since the
	// reconcile request is received upon change in the reconciler deployment
	// conditions.
	// Here we are checking for false condition which requires atleast 2 reconcile
	// request to be processed by the controller.
	err := nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, repoSyncID.Namespace,
		[]testpredicates.Predicate{
			hasReconcilingStatus(metav1.ConditionFalse),
			hasStalledStatus(metav1.ConditionFalse),
		})
	if err != nil {
		nt.T.Errorf("RepoSync did not finish reconciling: %v", err)
	}

	// Delete namespace reconciler deployment in bookstore namespace.
	// The point here is to test that we properly respond to kubectl commands,
	// so this should NOT be replaced with nt.Delete.
	nt.MustKubectl("delete", "deployment", nsReconciler,
		"-n", configsync.ControllerNamespace)

	// Verify that the deployment is re-created after deletion by checking the
	// Reconciling and Stalled condition in RepoSync resource.
	err = nt.Watcher.WatchObject(kinds.RepoSyncV1Beta1(), repoSyncID.Name, repoSyncID.Namespace,
		[]testpredicates.Predicate{
			hasReconcilingStatus(metav1.ConditionFalse),
			hasStalledStatus(metav1.ConditionFalse),
		})
	if err != nil {
		nt.T.Errorf("RepoSync did not finish reconciling: %v", err)
	}

	rootSyncLabels, err := nomostest.MetricLabelsForRootSync(nt, rootSyncKey)
	if err != nil {
		nt.T.Fatal(err)
	}
	repoSyncLabels, err := nomostest.MetricLabelsForRepoSync(nt, repoSyncKey)
	if err != nil {
		nt.T.Fatal(err)
	}
	rootCommitHash := rootSyncGitRepo.MustHash(nt.T)
	nnCommitHash := repoSyncGitRepo.MustHash(nt.T)

	// Skip sync & ops metrics and just validate reconciler-manager and reconciler errors.
	err = nomostest.ValidateMetrics(nt,
		nomostest.ReconcilerManagerMetrics(nt),
		nomostest.ReconcilerErrorMetrics(nt, rootSyncLabels, rootCommitHash, metrics.ErrorSummary{}),
		nomostest.ReconcilerErrorMetrics(nt, repoSyncLabels, nnCommitHash, metrics.ErrorSummary{}))
	if err != nil {
		nt.T.Fatal(err)
	}
}
