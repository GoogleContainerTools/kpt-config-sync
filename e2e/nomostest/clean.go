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

	"github.com/pkg/errors"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apiregistrationv1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1"
	"kpt.dev/configsync/e2e"
	"kpt.dev/configsync/e2e/nomostest/kubevirt/v1"
	"kpt.dev/configsync/e2e/nomostest/taskgroup"
	"kpt.dev/configsync/e2e/nomostest/testing"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/util"
	"kpt.dev/configsync/pkg/webhook/configuration"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestLabel is the label added to all test objects, ensuring we can clean up
// non-ephemeral clusters when tests are complete.
const TestLabel = "nomos-test"

// TestLabelValue is the value assigned to the above label.
const TestLabelValue = "enabled"

// AddTestLabel is automatically added to objects created or declared with the
// NT methods, or declared with Repository.Add.
//
// This isn't perfect - objects added via other means (such as kubectl) will
// bypass this.
var AddTestLabel = core.Label(TestLabel, TestLabelValue)

// Clean removes all objects of types registered in the scheme, with the above
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

	// Delete remote repos that were created 24 hours ago on the Git provider.
	if err := nt.GitProvider.DeleteObsoleteRepos(); err != nil {
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
	// Delete the Kubevirt resources
	if err := deleteKubevirt(nt); err != nil {
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
	// Delete namespaces with the detach annotation
	return deleteImplicitNamespacesAndWait(nt)
}

// deleteTestObjectsAndWait deletes all resource objects with the following constraints:
// - Has the test label (nomos-test: enabled)
// - Is cluster-scoped (namespace-scoped objects deleted by garbage collector)
// - Is not a resource managed by Autopilot
// - Is Listable (excludes some internal objects)
// - Is a resource type registered with nt.scheme.
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
		if !isListable(gvk.Kind) {
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
	return deleteObjectsAndWait(nt, objs...)
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
	if err := nt.List(nsList, withLabelListOption(metadata.ManagedByKey, metadata.ManagedByValue)); err != nil {
		return err
	}

	// Error if any protected namespaces are managed by config sync.
	// These must be cleaned up by the test.
	protectedNamespacesWithTestLabel, nsListItems := filterNamespaces(nsList.Items,
		protectedNamespaces...)
	if len(protectedNamespacesWithTestLabel) > 0 {
		return errors.Errorf("protected namespace(s) still have config sync labels and cannot be cleaned: %+v",
			protectedNamespacesWithTestLabel)
	}
	return deleteNamespacesAndWait(nt, nsListItems)
}

// deleteImplicitNamespacesAndWait deletes the namespaces with the PreventDeletion annotation.
func deleteImplicitNamespacesAndWait(nt *NT) error {
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		nt.T.Logf("[CLEANUP] Deleting implicit namespaces took %v", elapsed)
	}()

	nsList := &corev1.NamespaceList{}
	if err := nt.List(nsList); err != nil {
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
		return errors.Errorf("protected namespace(s) still have config sync annotations and cannot be cleaned: %+v",
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
	return deleteObjectsAndWait(nt, objs...)
}

func filterMutableListTypes(nt *NT) map[schema.GroupVersionKind]reflect.Type {
	allTypes := nt.scheme.AllKnownTypes()
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
	return key == TestLabel || strings.HasPrefix(key, "test-")
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

	ns.Annotations = annotations
	ns.Labels = labels

	return nt.Client.Update(nt.Context, ns)
}

func namespaceHasNoConfigSyncAnnotationsAndLabels() Predicate {
	return func(o client.Object) error {
		if o == nil {
			return ErrObjectNotFound
		}
		ns, ok := o.(*corev1.Namespace)
		if !ok {
			return WrongTypeErr(ns, &corev1.Namespace{})
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
	if err := nt.List(nsList); err != nil {
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

// deleteKubevirt cleans up the Kubevirt resources when the kubevirt namespace
// is stuck in the `Terminating` phase.
func deleteKubevirt(nt *NT) error {
	nt.T.Log("[CLEANUP] Cleaning up kubevirt resources")
	start := time.Now()
	defer func() {
		elapsed := time.Since(start)
		nt.T.Logf("[CLEANUP] Cleaning up kubevirt resources took %v", elapsed)
	}()

	kubevirtNS := corev1.Namespace{}
	if err := nt.Get("kubevirt", "", &kubevirtNS); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
	} else {
		// Remove finalizers on KubeVirt object to skip cleanup, if it exists.
		// WARNING: This may leave behind GCP resources on GKE clusters that
		// support nested virtualization, like those with N2 nodes.
		// TODO: Find a safer way to clean up KubeVirt...
		obj := kubevirt.NewKubeVirtObject()
		obj.SetName("kubevirt")
		obj.SetNamespace("kubevirt")
		if err := nt.MergePatch(obj, `{"metadata":{"finalizers":null}}`); err != nil {
			if !apierrors.IsNotFound(err) {
				return err
			}
		}
	}

	var objs []client.Object
	var obj client.Object

	obj = &corev1.Namespace{}
	obj.SetName("kubevirt")
	objs = append(objs, obj)

	obj = &apiregistrationv1.APIService{}
	obj.SetName("v1.subresources.kubevirt.io")
	objs = append(objs, obj)

	obj = &apiregistrationv1.APIService{}
	obj.SetName("v1alpha3.subresources.kubevirt.io")
	objs = append(objs, obj)

	// Mutating webhook objects are managed by Autopilot, so skip the cleanup if running on Autopilot clusters
	if !nt.IsGKEAutopilot {
		obj = &admissionregistrationv1.MutatingWebhookConfiguration{}
		obj.SetName("virt-api-mutator")
		objs = append(objs, obj)
	}

	obj = &admissionregistrationv1.ValidatingWebhookConfiguration{}
	obj.SetName("virt-api-mutator")
	objs = append(objs, obj)

	obj = &admissionregistrationv1.ValidatingWebhookConfiguration{}
	obj.SetName("virt-operator-validator")
	objs = append(objs, obj)

	// Delete specified KubeVirt objects in parallel and wait for NotFound
	if err := deleteObjectsAndWait(nt, objs...); err != nil {
		return err
	}

	// Delete all KubeVirt labelled CRDs and wait for NotFound
	crdList := &apiextensionsv1.CustomResourceDefinitionList{}
	if err := nt.List(crdList, withLabelListOption("app.kubernetes.io/component", "kubevirt")); err != nil {
		return err
	}
	if len(crdList.Items) > 0 {
		var crdNames []string
		for _, item := range crdList.Items {
			crdNames = append(crdNames, item.Name)
		}
		if err := WaitForCRDsNotFound(nt, crdNames); err != nil {
			return err
		}
	}
	return nil
}

// deleteAdmissionWebhook deletes the `admission-webhook.configsync.gke.io` ValidatingWebhookConfiguration.
func deleteAdmissionWebhook(nt *NT) error {
	obj := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	obj.SetName(configuration.Name)
	return deleteObject(nt, obj)
}

// deleteResourceGroupController deletes namespace `resource-group-system`.
func deleteResourceGroupController(nt *NT) error {
	obj := &corev1.Namespace{}
	obj.SetName(configmanagement.RGControllerNamespace)
	return deleteObject(nt, obj)
}

func deleteReconcilerManager(nt *NT) error {
	obj := &appsv1.Deployment{}
	obj.Namespace = configmanagement.ControllerNamespace
	obj.Name = reconcilermanager.ManagerName
	return deleteObject(nt, obj)
}

func deleteReconcilersBySyncKind(nt *NT, kind string) error {
	var opts []client.ListOption
	opts = append(opts, client.InNamespace(configmanagement.ControllerNamespace))
	opts = append(opts, withLabelListOption(metadata.SyncKindLabel, kind))
	dList := &appsv1.DeploymentList{}
	if err := nt.List(dList, opts...); err != nil {
		return err
	}
	for _, item := range dList.Items {
		if err := deleteObject(nt, &item); err != nil {
			return err
		}
	}
	return nil
}

func listObjectsWithTestLabel(nt *NT, gvk schema.GroupVersionKind) ([]unstructured.Unstructured, error) {
	list := &unstructured.UnstructuredList{}
	list.GetObjectKind().SetGroupVersionKind(gvk)
	if err := nt.List(list, withLabelListOption(TestLabel, TestLabelValue)); err != nil {
		if meta.IsNoMatchError(err) {
			// Resource not registered. So none can exist.
			return nil, nil
		}
		return nil, err
	}
	return list.Items, nil
}

func deleteObject(nt *NT, obj client.Object) error {
	nn := client.ObjectKeyFromObject(obj)
	gvk, err := kinds.Lookup(obj, nt.scheme)
	if err != nil {
		return err
	}
	if !obj.GetDeletionTimestamp().IsZero() {
		// already terminating. skip deleting
		return nil
	}
	if err = nt.Delete(obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
		if !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "unable to delete %s object %s",
				gvk.Kind, nn)
		}
	}
	return nil
}

// deleteObjectsAndWait deletes zero or more objects in serial and waits for not found in parallel.
// NOTE: Deleting in parallel might be faster, but the log would be harder to debug.
func deleteObjectsAndWait(nt *NT, objs ...client.Object) error {
	tg := taskgroup.New()
	for _, obj := range objs {
		nn := client.ObjectKeyFromObject(obj)
		gvk, err := kinds.Lookup(obj, nt.scheme)
		if err != nil {
			return err
		}
		if !obj.GetDeletionTimestamp().IsZero() {
			nt.T.Logf("[CLEANUP] already terminating %s object %s ...", gvk.Kind, nn)
		} else {
			nt.T.Logf("[CLEANUP] deleting %s object %s ...", gvk.Kind, nn)
			if err := nt.Delete(obj, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
				if apierrors.IsNotFound(err) {
					// skip waiting
					continue
				}
				return errors.Wrapf(err, "unable to delete %s object %s",
					gvk.Kind, nn)
			}
		}
		tg.Go(func() error {
			nt.T.Logf("[CLEANUP] Waiting for deletion of %s object %s ...", gvk.Kind, nn)
			return WatchForNotFound(nt, gvk, nn.Name, nn.Namespace)
		})
	}
	return tg.Wait()
}

func isListable(kind string) bool {
	// Only try to list types that have *List types associated with them, as they
	// are guaranteed to be listable.
	//
	// StatusList types are vestigial, have odd semantics, and are deprecated in 1.19.
	// Also we don't care about them for tests.
	return strings.HasSuffix(kind, "List") && !strings.HasSuffix(kind, "StatusList")
}

// FailIfUnknown fails the test if the passed type is not declared in the passed
// scheme.
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
	if err := nt.List(rsList); err != nil {
		if meta.IsNoMatchError(err) {
			// RepoSync resource not registered. So none can exist.
			return nil
		}
		return err
	}
	for _, rs := range rsList.Items {
		if IsDeletionPropagationEnabled(&rs) {
			rsCopy := rs.DeepCopy()
			// Disable deletion-propagation and delete finalizers
			// This should unblock RepoSync & Namespace deletion.
			RemoveDeletionPropagationPolicy(rsCopy)
			rsCopy.Finalizers = nil
			if err := nt.Update(rsCopy); err != nil {
				return err
			}
		}
	}
	return nil
}

func disableRootSyncDeletionPropagation(nt *NT) error {
	// List all RootSyncs in all namespaces
	rsList := &v1beta1.RootSyncList{}
	if err := nt.List(rsList); err != nil {
		if meta.IsNoMatchError(err) {
			// RootSync resource not registered. So none can exist.
			return nil
		}
		return err
	}
	for _, rs := range rsList.Items {
		if IsDeletionPropagationEnabled(&rs) {
			rsCopy := rs.DeepCopy()
			// Disable deletion-propagation and delete finalizers
			// This should unblock RootSync & `config-management-system` Namespace deletion.
			RemoveDeletionPropagationPolicy(rsCopy)
			rsCopy.Finalizers = nil
			if err := nt.Update(rsCopy); err != nil {
				return err
			}
		}
	}
	return nil
}
