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

package nomostest

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/e2e/nomostest/testkubeclient"
	"kpt.dev/configsync/e2e/nomostest/testpredicates"
	"kpt.dev/configsync/e2e/nomostest/testwatcher"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reconcilermanager/controllers"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/util"
	"kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/cli-utils/pkg/common"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// ConfigSyncE2EFinalizer is a contrived finalizer to block deletion of
	// objects created by the e2e tests.
	ConfigSyncE2EFinalizer = "config-sync/e2e-test"
)

// Clean removes all objects of types registered in the Scheme, with the above
// caveats. It should be run before and after a test is run against any
// non-ephemeral cluster.
//
// It is unnecessary to run this on Kind clusters that exist only for the
// duration of a single test.
func Clean(nt *NT) error {
	nt.T.Helper()
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		nt.T.Logf("[CLEANUP] Test environment cleanup took %v", elapsed)
	}()

	// If the reconciler-manager is running, attempt to uninstall packages.
	// If not running, continue with more aggressive cleanup.
	// This should help avoid deletion ordering edge cases.
	running, err := isReconcilerManagerHealthy(nt)
	if err != nil {
		return err
	}
	if running {
		if err := uninstallAllPackages(nt); err != nil {
			nt.T.Errorf("[CLEANUP] failed to uninstall packages: %v", err)
		}
	}
	// Delete lingering APIService, as this will cause API discovery to fail
	if err := deleteTestAPIServices(nt); err != nil {
		return err
	}
	// Delete remote repos that were created 24 hours ago on the Git provider.
	if err := nt.GitProvider.DeleteObsoleteRepos(); err != nil {
		return err
	}
	// Uninstall prometheus components
	if err := uninstallPrometheus(nt); err != nil {
		return err
	}
	// The admission-webhook prevents deleting test resources. Hence we delete it before cleaning other resources.
	if err := deleteAdmissionWebhook(nt); err != nil {
		return err
	}
	// Delete the reconciler-manager to stop reconcilers from being updated/re-created
	if err := deleteReconcilerManager(nt); err != nil {
		return err
	}
	// Delete the reconcilers to stop syncing
	if err := deleteReconcilersBySyncKind(nt, configsync.RepoSyncKind); err != nil {
		return err
	}
	if err := deleteReconcilersBySyncKind(nt, configsync.RootSyncKind); err != nil {
		return err
	}
	// Delete RSync finalizers to unblock namespace deletion.
	if err := disableRepoSyncDeletionPropagation(nt); err != nil {
		return err
	}
	if err := disableRootSyncDeletionPropagation(nt); err != nil {
		return err
	}
	// Completely uninstall any remaining config-sync components
	if err := uninstallConfigSync(nt); err != nil {
		return err
	}
	// Delete the resource-group-system namespace
	if err := deleteResourceGroupController(nt); err != nil {
		return err
	}
	// Reset any modified system namespaces.
	if err := resetSystemNamespaces(nt); err != nil {
		return err
	}
	// Delete objects with the test label
	if err := deleteTestObjectsAndWait(nt); err != nil {
		return err
	}
	// Delete namespaces with the managed-by label
	if err := deleteManagedNamespacesAndWait(nt); err != nil {
		return err
	}
	// Delete ClusterRoleBindings managed by reconciler-manager
	if err := deleteManagedClusterRoleBindingsAndWait(nt); err != nil {
		return err
	}
	// Delete namespaces with the detach annotation
	return deleteImplicitNamespacesAndWait(nt)
}

func isReconcilerManagerHealthy(nt *NT) (bool, error) {
	// Check if reconciler-manager is reconciled and healthy
	nt.T.Log("[CLEANUP] checking reconciler-manager status...")
	rmObj := &unstructured.Unstructured{}
	rmObj.SetGroupVersionKind(kinds.Deployment())
	if err := nt.KubeClient.Get(reconcilermanager.ManagerName, configmanagement.ControllerNamespace, rmObj); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking reconciler-manager status: %v", err)
	}
	if err := testpredicates.StatusEquals(nt.Scheme, kstatus.CurrentStatus)(rmObj); err != nil {
		nt.T.Logf("[CLEANUP] reconciler-manager installed but not healthy: %v", err)
		return false, nil
	}
	return true, nil
}

// uninstallAllPackages uninstalls all the unmanaged RootSyncs & RepoSyncs.
// Assuming the managed RSyncs are managed by the unmanaged RSyncs, and all the
// packages have deletion-propagation enabled, all the packages should be
// uninstalled.
func uninstallAllPackages(nt *NT) error {
	for _, gvk := range []schema.GroupVersionKind{kinds.RepoSyncV1Beta1(), kinds.RootSyncV1Beta1()} {
		if err := uninstallUnmanagedPackagesOfType(nt, gvk); err != nil {
			return fmt.Errorf("uninstalling unmanged %s: %v", gvk.Kind, err)
		}
	}
	return nil
}

func uninstallUnmanagedPackagesOfType(nt *NT, gvk schema.GroupVersionKind) error {
	// Find all packages
	nt.T.Logf("[CLEANUP] Listing %s objects...", gvk.Kind)
	rsObjList := &unstructured.UnstructuredList{}
	rsObjList.SetGroupVersionKind(kinds.ListGVKForItemGVK(gvk))
	if err := nt.KubeClient.List(rsObjList, client.InNamespace(configmanagement.ControllerNamespace)); err != nil {
		return fmt.Errorf("listing %s: %v", gvk.Kind, err)
	}
	// Find the unmanaged packages
	var rsObjs []*unstructured.Unstructured
	for _, item := range rsObjList.Items {
		if metadata.IsManagementEnabled(&item) {
			nt.T.Logf("[CLEANUP] Skipping deletion of managed %s object %s",
				gvk.Kind, client.ObjectKeyFromObject(&item))
		} else {
			rsObjs = append(rsObjs, &item)
		}
	}
	// Once deleted, one of the following conditions must be satisfied to continue
	conditions := []testpredicates.Predicate{
		testpredicates.ObjectNotFoundPredicate(nt.Scheme),
		testpredicates.StatusEquals(nt.Scheme, kstatus.FailedStatus),
	}
	switch gvk {
	case kinds.RootSyncV1Beta1():
		conditions = append(conditions, testpredicates.HasConditionStatus(nt.Scheme,
			string(v1beta1.RootSyncReconcilerFinalizerFailure), corev1.ConditionTrue))
	case kinds.RepoSyncV1Beta1():
		conditions = append(conditions, testpredicates.HasConditionStatus(nt.Scheme,
			string(v1beta1.RepoSyncReconcilerFinalizerFailure), corev1.ConditionTrue))
	}
	watchOptions := []testwatcher.WatchOption{
		testwatcher.WatchPredicates(testpredicates.Or(nt.Logger, conditions...)),
	}
	// Delete the unmanaged packages in parallel
	nt.T.Logf("[CLEANUP] Deleting unmanaged %s objects...", gvk.Kind)
	tg := taskgroup.New()
	for _, obj := range rsObjs {
		nn := client.ObjectKeyFromObject(obj)
		if !obj.GetDeletionTimestamp().IsZero() {
			nt.T.Logf("[CLEANUP] Already terminating %s object %s ...", gvk.Kind, nn)
		} else {
			nt.T.Logf("[CLEANUP] Deleting %s object %s ...", gvk.Kind, nn)
			if err := nt.KubeClient.Delete(obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
					nt.T.Logf("[CLEANUP] Confirmed removal of %s object %s", gvk.Kind, nn)
				} else {
					// return an error, but don't prevent other deletes or waits
					tg.Go(func() error {
						return fmt.Errorf("unable to delete %s object %s: %w",
							gvk.Kind, nn, err)
					})
				}
				// skip waiting
				continue
			}
		}
		tg.Go(func() error {
			nt.T.Logf("[CLEANUP] Waiting for removal or failure of %s object %s ...", gvk.Kind, nn)
			err := nt.Watcher.WatchObject(gvk, nn.Name, nn.Namespace, watchOptions...)
			if err == nil {
				nt.T.Logf("[CLEANUP] Confirmed removal of %s object %s", gvk.Kind, nn)
			}
			return err
		})
	}
	// Wait for the unmanaged packages to be deleted or fail or timeout
	if err := tg.Wait(); err != nil {
		return err
	}
	nt.T.Logf("[CLEANUP] Successfully uninstalled all %s objects", gvk.Kind)
	return nil
}

// deleteTestObjectsAndWait deletes all resource objects with the following constraints:
// - Has the test label (nomos-test: enabled)
// - Is cluster-scoped (namespace-scoped objects deleted by garbage collector)
// - Is not a resource managed by Autopilot
// - Is Listable (excludes some internal objects)
// - Is a resource type registered with nt.Scheme.
//
// After deletion, the deleted objects are watched until they are Not Found.
//
// WARNING: Delegating to the namespace garbage collector is only safe if all
// objects with same-namespace deletion dependencies have already been deleted
// in the proper order. Otherwise namespace deletion may deadlock.
func deleteTestObjectsAndWait(nt *NT) error {
	// Build a list of objects of known mutable resource objects
	var objs []client.Object
	types := filterMutableListTypes(nt)
	for gvk := range types {
		if !isListable(gvk) {
			continue
		}
		list, err := listObjectsWithTestLabel(nt, gvk)
		if err != nil {
			return err
		}
		if len(list) > 0 && list[0].GetNamespace() != "" {
			// Ignore namespace-scoped resources
			continue
		}
		for i := range list {
			objs = append(objs, &list[i])
		}
	}
	// Delete all objects in serial and wait for not found in parallel.
	return DeleteObjectsAndWait(nt, objs...)
}

// deleteManagedNamespacesAndWait deletes all the namespaces managed by Config Sync.
func deleteManagedNamespacesAndWait(nt *NT) error {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		nt.T.Logf("[CLEANUP] Deleting managed namespaces took %v", elapsed)
	}()

	// Delete all Namespaces with the test label (except protected).
	nsList := &corev1.NamespaceList{}
	if err := nt.KubeClient.List(nsList, withLabelListOption(metadata.ManagedByKey, metadata.ManagedByValue)); err != nil {
		return err
	}

	// Error if any protected namespaces are managed by config sync.
	// These must be cleaned up by the test.
	protectedNamespacesWithTestLabel, nsListItems := filterNamespaces(nsList.Items,
		protectedNamespaces...)
	if len(protectedNamespacesWithTestLabel) > 0 {
		return fmt.Errorf("protected namespace(s) still have config sync labels and cannot be cleaned: %+v",
			protectedNamespacesWithTestLabel)
	}
	return deleteNamespacesAndWait(nt, nsListItems)
}

// deleteManagedClusterRoleBindingsAndWait deletes all the ClusterRoleBindings
// managed by reconciler-manager.
// All other objects managed by reconciler-manager are namespace-scoped and
// should get cleaned up by namespace deletion. A ClusterRoleBinding can be
// orphaned if the RootSync finalizer is force removed before the finalizer
// completes.
func deleteManagedClusterRoleBindingsAndWait(nt *NT) error {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		nt.T.Logf("[CLEANUP] Deleting managed ClusterRoleBindings took %v", elapsed)
	}()

	// Delete all ClusterRoleBindings with the "managed by reconciler-manager" label
	opts := &client.ListOptions{}
	opts.LabelSelector = client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(controllers.ManagedByLabel()),
	}
	crbList := &rbacv1.ClusterRoleBindingList{}
	if err := nt.KubeClient.List(crbList, opts); err != nil {
		return err
	}

	if len(crbList.Items) == 0 {
		return nil
	}
	var objs []client.Object
	for i := range crbList.Items {
		objs = append(objs, &crbList.Items[i])
	}
	return DeleteObjectsAndWait(nt, objs...)
}

// deleteImplicitNamespacesAndWait deletes the namespaces with the PreventDeletion annotation.
func deleteImplicitNamespacesAndWait(nt *NT) error {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		nt.T.Logf("[CLEANUP] Deleting implicit namespaces took %v", elapsed)
	}()

	nsList := &corev1.NamespaceList{}
	if err := nt.KubeClient.List(nsList); err != nil {
		return err
	}
	var nsListFiltered []corev1.Namespace
	for i, ns := range nsList.Items {
		if annotation, ok := ns.Annotations[common.LifecycleDeleteAnnotation]; ok && annotation == common.PreventDeletion {
			nsListFiltered = append(nsListFiltered, nsList.Items[i])
		}
	}
	// Error if any protected namespace still have config sync annotations.
	// These must be cleaned up by the test.
	protectedNamespacesWithTestLabel, nsListFiltered := filterNamespaces(nsListFiltered,
		protectedNamespaces...)
	if len(protectedNamespacesWithTestLabel) > 0 {
		return fmt.Errorf("protected namespace(s) still have config sync annotations and cannot be cleaned: %+v",
			protectedNamespacesWithTestLabel)
	}
	return deleteNamespacesAndWait(nt, nsListFiltered)
}

// deleteNamespacesAndWait deletes zero or more Namespaces and waits for them to be
// not found. Namespace deletion invokes the garbage collector, which will
// delete all objects in that namespace.
func deleteNamespacesAndWait(nt *NT, nsList []corev1.Namespace) error {
	nt.T.Logf("[CLEANUP] Deleting Namespaces (%d)", len(nsList))
	if len(nsList) == 0 {
		return nil
	}
	var objs []client.Object
	for i := range nsList {
		objs = append(objs, &nsList[i])
	}
	return DeleteObjectsAndWait(nt, objs...)
}

func filterMutableListTypes(nt *NT) map[schema.GroupVersionKind]reflect.Type {
	allTypes := nt.Scheme.AllKnownTypes()
	if nt.IsGKEAutopilot {
		for _, t := range util.AutopilotManagedKinds {
			delete(allTypes, t)
		}
	}
	return allTypes
}

func isConfigSyncAnnotation(annotation string) bool {
	return annotation == common.LifecycleDeleteAnnotation ||
		strings.Contains(annotation, metadata.ConfigManagementPrefix) ||
		strings.Contains(annotation, configsync.ConfigSyncPrefix) ||
		annotation == metadata.OwningInventoryKey ||
		annotation == metadata.HNCManagedBy
}

func isTestAnnotation(annotation string) bool {
	return strings.HasPrefix(annotation, "test-")
}

func isConfigSyncLabel(key, value string) bool {
	return (key == metadata.ManagedByKey && value == metadata.ManagedByValue) || key == metadata.DeclaredVersionLabel || strings.Contains(key, metadata.DepthSuffix)
}

func isTestLabel(key string) bool {
	return key == testkubeclient.TestLabel || strings.HasPrefix(key, "test-")
}

// deleteConfigSyncAndTestAnnotationsAndLabels removes config sync and test annotations and labels from the namespace and update it.
func deleteConfigSyncAndTestAnnotationsAndLabels(nt *NT, ns *corev1.Namespace) error {
	var annotations = make(map[string]string)
	var labels = make(map[string]string)
	for k, v := range ns.Annotations {
		if !isConfigSyncAnnotation(k) && !isTestAnnotation(k) {
			annotations[k] = v
		}
	}
	for k, v := range ns.Labels {
		if !isConfigSyncLabel(k, v) && !isTestLabel(k) {
			labels[k] = v
		}
	}
	// return without updating if no annotations/labels were removed
	if len(annotations) == len(ns.Annotations) && len(labels) == len(ns.Labels) {
		return nil
	}

	ns.Annotations = annotations
	ns.Labels = labels

	return nt.KubeClient.Update(ns)
}

func namespaceHasNoConfigSyncAnnotationsAndLabels() testpredicates.Predicate {
	return func(o client.Object) error {
		if o == nil {
			return testpredicates.ErrObjectNotFound
		}
		ns, ok := o.(*corev1.Namespace)
		if !ok {
			return testpredicates.WrongTypeErr(ns, &corev1.Namespace{})
		}
		for k := range ns.Annotations {
			if isConfigSyncAnnotation(k) {
				return fmt.Errorf("found config sync annotation %s in namespace %s", k, ns.Name)
			}
		}
		for k, v := range ns.Labels {
			if isConfigSyncLabel(k, v) {
				return fmt.Errorf("found config sync label `%s: %s` in namespace %s", k, v, ns.Name)
			}
		}
		return nil
	}
}

// resetSystemNamespaces removes the config sync annotations and labels from the system namespaces.
func resetSystemNamespaces(nt *NT) error {
	nsList := &corev1.NamespaceList{}
	if err := nt.KubeClient.List(nsList); err != nil {
		return err
	}
	for _, ns := range nsList.Items {
		ns.SetGroupVersionKind(kinds.Namespace())
		// Skip mutating the GKE Autopilot cluster managed namespaces.
		if nt.IsGKEAutopilot && util.IsAutopilotManagedNamespace(&ns) {
			continue
		}
		if differ.IsManageableSystemNamespace(&ns) {
			if err := deleteConfigSyncAndTestAnnotationsAndLabels(nt, &ns); err != nil {
				return err
			}
			if err := nt.Validate(ns.Name, "", &corev1.Namespace{}, namespaceHasNoConfigSyncAnnotationsAndLabels()); err != nil {
				return err
			}
		}
	}
	return nil
}

// deleteAdmissionWebhook deletes the `admission-webhook.configsync.gke.io` ValidatingWebhookConfiguration.
func deleteAdmissionWebhook(nt *NT) error {
	obj := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	obj.SetName(configuration.Name)
	return DeleteObjectsAndWait(nt, obj)
}

// deleteResourceGroupController deletes namespace `resource-group-system`.
func deleteResourceGroupController(nt *NT) error {
	obj := &corev1.Namespace{}
	obj.SetName(configmanagement.RGControllerNamespace)
	return DeleteObjectsAndWait(nt, obj)
}

func deleteReconcilerManager(nt *NT) error {
	obj := &appsv1.Deployment{}
	obj.Namespace = configmanagement.ControllerNamespace
	obj.Name = reconcilermanager.ManagerName
	return DeleteObjectsAndWait(nt, obj)
}

func deleteReconcilersBySyncKind(nt *NT, kind string) error {
	var opts []client.ListOption
	opts = append(opts, client.InNamespace(configmanagement.ControllerNamespace))
	opts = append(opts, withLabelListOption(metadata.SyncKindLabel, kind))
	dList := &appsv1.DeploymentList{}
	if err := nt.KubeClient.List(dList, opts...); err != nil {
		return err
	}
	return DeleteObjectListItemsAndWait(nt, dList)
}

func listObjectsWithTestLabel(nt *NT, gvk schema.GroupVersionKind) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.GetObjectKind().SetGroupVersionKind(gvk)
	if err := nt.KubeClient.List(list, withLabelListOption(testkubeclient.TestLabel, testkubeclient.TestLabelValue)); err != nil {
		if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
			// Resource not registered. So none can exist.
			return nil, nil
		}
		return nil, err
	}
	return list.Items, nil
}

// DeleteObjectsAndWait deletes zero or more objects in serial and waits for not found in parallel.
// NOTE: Deleting in parallel might be faster, but the log would be harder to debug.
func DeleteObjectsAndWait(nt *NT, objs ...client.Object) error {
	tg := taskgroup.New()
	for _, obj := range objs {
		nn := client.ObjectKeyFromObject(obj)
		gvk, err := kinds.Lookup(obj, nt.Scheme)
		if err != nil {
			tg.Go(func() error {
				return err
			})
			continue
		}
		// Remove fake test finalizers if they are blocking deletion
		if reflect.DeepEqual(obj.GetFinalizers(), []string{ConfigSyncE2EFinalizer}) {
			if err := nt.KubeClient.MergePatch(obj, `{"metadata":{"finalizers":[]}}`); err != nil {
				if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
					// skip waiting
					continue
				}
				tg.Go(func() error {
					return err
				})
				continue
			}
		}
		if !obj.GetDeletionTimestamp().IsZero() {
			nt.T.Logf("[CLEANUP] already terminating %s object %s ...", gvk.Kind, nn)
		} else {
			nt.T.Logf("[CLEANUP] deleting %s object %s ...", gvk.Kind, nn)
			if err := nt.KubeClient.Delete(obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
					// skip waiting
					continue
				}
				tg.Go(func() error {
					return fmt.Errorf("unable to delete %s object %s: %w",
						gvk.Kind, nn, err)
				})
				continue
			}
		}
		tg.Go(func() error {
			nt.T.Logf("[CLEANUP] Waiting for deletion of %s object %s ...", gvk.Kind, nn)
			return nt.Watcher.WatchForNotFound(gvk, nn.Name, nn.Namespace)
		})
	}
	return tg.Wait()
}

// DeleteObjectListItemsAndWait deletes all the list items in serial and waits
// for not found in parallel.
func DeleteObjectListItemsAndWait(nt *NT, objList client.ObjectList) error {
	objs, err := kinds.ExtractClientObjectList(objList)
	if err != nil {
		return err
	}
	return DeleteObjectsAndWait(nt, objs...)
}

func isListable(listGVK schema.GroupVersionKind) bool {
	// Only try to list types that have *List types associated with them, as they
	// are guaranteed to be listable.
	//
	// StatusList types are vestigial, have odd semantics, and are deprecated in 1.19.
	// Also we don't care about them for tests.
	return kinds.IsListGVK(listGVK) && !strings.HasSuffix(listGVK.Kind, "StatusList")
}

// FailIfUnknown fails the test if the passed type is not declared in the passed
// Scheme.
func FailIfUnknown(t testing.NTB, scheme *runtime.Scheme, o client.Object) {
	t.Helper()

	gvks, _, _ := scheme.ObjectKinds(o)
	if len(gvks) == 0 {
		t.Fatalf("unknown type %T %v. Add it to nomostest.newScheme().", o, o.GetObjectKind().GroupVersionKind())
	}
}

// DeleteRemoteRepos removes all remote repos on the Git provider.
func DeleteRemoteRepos(nt *NT) {
	if nt.GitProvider.Type() == e2e.Local {
		return
	}

	var repos []string
	for _, repo := range nt.RemoteRepositories {
		repos = append(repos, repo.RemoteRepoName)
	}
	err := nt.GitProvider.DeleteRepositories(repos...)
	if err != nil {
		nt.T.Error(err)
	}
}

func disableRepoSyncDeletionPropagation(nt *NT) error {
	// List all RepoSyncs in all namespaces
	rsList := &v1beta1.RepoSyncList{}
	if err := nt.KubeClient.List(rsList); err != nil {
		if meta.IsNoMatchError(err) {
			// RepoSync resource not registered. So none can exist.
			return nil
		}
		return err
	}
	for _, rs := range rsList.Items {
		if metadata.HasDeletionPropagationPolicy(&rs, metadata.DeletionPropagationPolicyForeground) {
			rsCopy := rs.DeepCopy()
			// Disable deletion-propagation and delete finalizers
			// This should unblock RepoSync & Namespace deletion.
			metadata.RemoveDeletionPropagationPolicy(rsCopy)
			rsCopy.Finalizers = nil
			if err := nt.KubeClient.Update(rsCopy); err != nil {
				return err
			}
		}
	}
	return nil
}

func disableRootSyncDeletionPropagation(nt *NT) error {
	// List all RootSyncs in all namespaces
	rsList := &v1beta1.RootSyncList{}
	if err := nt.KubeClient.List(rsList); err != nil {
		if meta.IsNoMatchError(err) {
			// RootSync resource not registered. So none can exist.
			return nil
		}
		return err
	}
	for _, rs := range rsList.Items {
		if metadata.HasDeletionPropagationPolicy(&rs, metadata.DeletionPropagationPolicyForeground) {
			rsCopy := rs.DeepCopy()
			// Disable deletion-propagation and delete finalizers
			// This should unblock RootSync & `config-management-system` Namespace deletion.
			metadata.RemoveDeletionPropagationPolicy(rsCopy)
			rsCopy.Finalizers = nil
			if err := nt.KubeClient.Update(rsCopy); err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteTestAPIServices(nt *NT) error {
	list := &apiregistrationv1.APIServiceList{}
	if err := nt.KubeClient.List(list, withLabelListOption("testdata", "true")); err != nil {
		return err
	}
	return DeleteObjectListItemsAndWait(nt, list)
}
