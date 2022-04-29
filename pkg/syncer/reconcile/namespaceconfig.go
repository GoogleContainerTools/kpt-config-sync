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

package reconcile

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configmanagement"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/status"
	syncercache "kpt.dev/configsync/pkg/syncer/cache"
	syncerclient "kpt.dev/configsync/pkg/syncer/client"
	"kpt.dev/configsync/pkg/syncer/decode"
	"kpt.dev/configsync/pkg/syncer/differ"
	"kpt.dev/configsync/pkg/syncer/metrics"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const reconcileTimeout = time.Minute * 5

// reservedNamespaceConfig is a dummy namespace config used to represent the config content of
// non-removable namespaces (like "default") when the corresponding NamespaceConfig is deleted
// from the repo. Instead of deleting the namespace and its resources, we apply a change that
// removes all managed resources from the namespace, but does not attempt to delete the namespace.
// TODO: See if there is an easy way to hide this nil object.
var reservedNamespaceConfig = &v1.NamespaceConfig{
	TypeMeta: metav1.TypeMeta{
		Kind:       kinds.NamespaceConfig().Kind,
		APIVersion: kinds.NamespaceConfig().GroupVersion().String(),
	},
}

// NamespaceConfigReconciler reconciles a NamespaceConfig object.
type namespaceConfigReconciler struct {
	client   *syncerclient.Client
	applier  Applier
	cache    *syncercache.GenericCache
	recorder record.EventRecorder
	decoder  decode.Decoder
	now      func() metav1.Time
	toSync   []schema.GroupVersionKind
	//mgrInitTime is the subManager's instantiation time
	mgrInitTime metav1.Time
}

var _ reconcile.Reconciler = &namespaceConfigReconciler{}

// NewNamespaceConfigReconciler returns a new NamespaceConfigReconciler.
func NewNamespaceConfigReconciler(c *syncerclient.Client, applier Applier, reader client.Reader, recorder record.EventRecorder,
	decoder decode.Decoder, now func() metav1.Time, toSync []schema.GroupVersionKind, mgrInitTime metav1.Time) reconcile.Reconciler {
	return &namespaceConfigReconciler{
		client:      c,
		applier:     applier,
		cache:       syncercache.NewGenericResourceCache(reader),
		recorder:    recorder,
		decoder:     decoder,
		toSync:      toSync,
		now:         now,
		mgrInitTime: mgrInitTime,
	}
}

// Reconcile is the Reconcile callback for NamespaceConfigReconciler.
func (r *namespaceConfigReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	start := r.now()
	metrics.ReconcileEventTimes.WithLabelValues("namespace").Set(float64(start.Unix()))

	name := request.Name
	klog.V(2).Infof("Reconciling NamespaceConfig: %q", name)
	if configmanagement.IsControllerNamespace(name) {
		klog.Errorf("Trying to reconcile a NamespaceConfig corresponding to a reserved namespace: %q", name)
		// We don't return an error, because we should never be reconciling these NamespaceConfigs in the first place.
		return reconcile.Result{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, reconcileTimeout)
	defer cancel()

	err := r.reconcileNamespaceConfig(ctx, name)
	metrics.ReconcileDuration.WithLabelValues("namespace", metrics.StatusLabel(err)).Observe(time.Since(start.Time).Seconds())

	// Filter out errors caused by a context cancellation. These errors are expected and uninformative.
	if filtered := filterContextCancelled(err); filtered != nil {
		klog.Errorf("Could not reconcile namespaceconfig %q: %v", name, status.FormatSingleLine(filtered))
	}
	return reconcile.Result{}, err
}

// getNamespaceConfig normalizes the state of the NamespaceConfig and returns the config.
func (r *namespaceConfigReconciler) getNamespaceConfig(
	ctx context.Context,
	name string,
) *v1.NamespaceConfig {
	config := &v1.NamespaceConfig{}
	err := r.cache.Get(ctx, apitypes.NamespacedName{Name: name}, config)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		panic(errors.Wrap(err, "cache returned error other than not found, this should not happen"))
	}
	config.SetGroupVersionKind(kinds.NamespaceConfig())

	return config
}

// getNamespace normalizes the state of the namespace and retrieves the current value.
func (r *namespaceConfigReconciler) getNamespace(ctx context.Context, name string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := r.cache.Get(ctx, apitypes.NamespacedName{Name: name}, ns)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "got unhandled lister error")
	}

	return ns, nil
}

func invalidManagementLabel(invalid string) string {
	return fmt.Sprintf("Namespace has invalid management annotation %s=%s should be %q or unset",
		metadata.ResourceManagementKey, invalid, metadata.ResourceManagementEnabled)
}

func (r *namespaceConfigReconciler) reconcileNamespaceConfig(
	ctx context.Context,
	name string,
) error {
	config := r.getNamespaceConfig(ctx, name)

	ns, nsErr := r.getNamespace(ctx, name)
	if nsErr != nil {
		return nsErr
	}

	diff := differ.NamespaceDiff{
		Name:     name,
		Declared: config,
		Actual:   ns,
	}

	klog.V(3).Infof("ns:%q: diffType=%v", name, diff.Type())
	var syncErrs []v1.ConfigManagementError
	switch diff.Type() {
	case differ.Create:
		if err := r.createNamespace(ctx, config); err != nil {
			syncErrs = append(syncErrs, newSyncError(config, err))
			if err2 := r.setNamespaceConfigStatus(ctx, config, r.mgrInitTime, syncErrs, nil); err2 != nil {
				klog.Warningf("Failed to set status on NamespaceConfig after namespace creation error: %s", err2)
			}
			return err
		}
		return r.manageConfigs(ctx, name, config, syncErrs)

	case differ.Update:
		if err := r.updateNamespace(ctx, config, ns); err != nil {
			syncErrs = append(syncErrs, newSyncError(config, err))
		}
		return r.manageConfigs(ctx, name, config, syncErrs)

	case differ.Delete:
		return r.deleteNamespace(ctx, ns)

	case differ.UnmanageNamespace:
		return r.deleteManageableSystem(ctx, ns, config, syncErrs)

	case differ.DeleteNsConfig:
		return r.deleteNsConfig(ctx, config)

	case differ.Unmanage:
		// Remove defunct labels and annotations.
		unmanageErr := r.unmanageNamespace(ctx, ns)
		if unmanageErr != nil {
			klog.Warningf("Failed to remove management labels and annotations from namespace: %s", unmanageErr.Error())
			return unmanageErr
		}

		// Return an error if any encountered.
		return r.manageConfigs(ctx, name, config, syncErrs)

	case differ.Error:
		value := config.GetAnnotations()[metadata.ResourceManagementKey]
		klog.Warningf("Namespace %q has invalid management annotation %q", name, value)
		r.recorder.Eventf(
			config,
			corev1.EventTypeWarning,
			v1.EventReasonInvalidManagementAnnotation,
			"Namespace %q has invalid management annotation %q",
			name, value,
		)
		syncErrs = append(syncErrs, cmeForNamespace(ns, invalidManagementLabel(value)))
		return r.manageConfigs(ctx, name, config, syncErrs)

	case differ.NoOp:
		return r.manageConfigs(ctx, name, config, syncErrs)
	}
	panic(fmt.Sprintf("unhandled diff type: %v", diff.Type()))
}

// unmanageNamespace removes the nomos annotations and labels from the Namespace.
func (r *namespaceConfigReconciler) unmanageNamespace(ctx context.Context, actual *corev1.Namespace) error {
	uActual, err := AsUnstructuredSanitized(actual)
	if err != nil {
		return err
	}
	_, err = r.applier.RemoveNomosMeta(ctx, uActual)
	return err
}

func (r *namespaceConfigReconciler) manageConfigs(ctx context.Context, namespace string, config *v1.NamespaceConfig, syncErrs []v1.ConfigManagementError) error {
	if config == nil {
		return nil
	}

	if gks := resourcesWithoutSync(config.Spec.Resources, r.toSync); len(gks) != 0 {
		klog.Warningf("NamespaceConfig reconciler encountered GroupKinds that were not present in a sync: %s. Waiting for reconciler restart",
			strings.Join(gks, ", "))
		// We only reach this case on a race condition where the reconciler is run before the
		// changes to Sync objects are picked up.  We exit early since there are resources we can't
		// properly handle which will cause status on the NamespaceConfig to incorrectly report that
		// everything is fully synced.  We log info and return nil here since the Sync metacontroller
		// will restart this reconciler shortly.
		return nil
	}

	var errBuilder status.MultiError
	reconcileCount := 0
	grs, err := r.decoder.DecodeResources(config.Spec.Resources)
	if err != nil {
		return errors.Wrapf(err, "could not process namespaceconfig: %q", config.GetName())
	}

	var resConditions []v1.ResourceCondition

	for _, gvk := range r.toSync {
		declaredInstances := grs[gvk]
		for _, decl := range declaredInstances {
			core.SetAnnotation(decl, metadata.SyncTokenAnnotationKey, config.Spec.Token)
		}

		actualInstances, err := r.cache.UnstructuredList(ctx, gvk, client.InNamespace(namespace))
		if err != nil {
			errBuilder = status.Append(errBuilder, status.APIServerErrorf(err, "failed to list from NamespaceConfig controller for %q", gvk))
			syncErrs = append(syncErrs, newSyncError(config, err))
			continue
		}

		for _, act := range actualInstances {
			annotations := act.GetAnnotations()
			if annotationsHaveResourceCondition(annotations) {
				resConditions = append(resConditions, makeResourceCondition(*act, config.Spec.Token))
			}
		}

		allDeclaredVersions := AllVersionNames(grs, gvk.GroupKind())
		diffs := differ.Diffs(declaredInstances, actualInstances, allDeclaredVersions)
		for _, diff := range diffs {
			if updated, err := HandleDiff(ctx, r.applier, diff, r.recorder); err != nil {
				errBuilder = status.Append(errBuilder, err)
				syncErrs = append(syncErrs, err.ToCME())
			} else if updated {
				reconcileCount++
			}
		}
	}

	if err := r.setNamespaceConfigStatus(ctx, config, r.mgrInitTime, syncErrs, resConditions); err != nil {
		errBuilder = status.Append(errBuilder, status.APIServerErrorf(err, "failed to set status for NamespaceConfig %q", namespace))
		r.recorder.Eventf(config, corev1.EventTypeWarning, v1.EventReasonStatusUpdateFailed,
			"failed to update NamespaceConfig status: %s", err)
	}

	if errBuilder == nil && reconcileCount > 0 && len(syncErrs) == 0 {
		r.recorder.Eventf(config, corev1.EventTypeNormal, v1.EventReasonReconcileComplete,
			"NamespaceConfig %q was successfully reconciled: %d changes", namespace, reconcileCount)
	}
	return errBuilder
}

// needsUpdate returns true if the given NamespaceConfig will need a status update with the other given arguments.
// initTime is the syncer-controller's instantiation time. It skips updating the sync state if the
// import time is stale (not after the init time or not after the deleteSyncedTime) so the
// repostatus-reconciler can force-update everything on startup.
// An update is needed
// - if the sync state is not synced, or
// - if the sync time is stale (not after the init time or not after the deleteSyncedTime), or
// - if errors occur, or
// - if resourceConditions are not empty.
func needsUpdate(config *v1.NamespaceConfig, initTime metav1.Time, errs []v1.ConfigManagementError, resConditions []v1.ResourceCondition) bool {
	if config == reservedNamespaceConfig {
		return false
	}
	if !config.Spec.ImportTime.After(initTime.Time) || !config.Spec.ImportTime.After(config.Spec.DeleteSyncedTime.Time) {
		klog.V(3).Infof("Ignoring previously imported config %q", config.Name)
		return false
	}
	return !config.Status.SyncState.IsSynced() ||
		config.Status.Token != config.Spec.Token ||
		!config.Status.SyncTime.After(initTime.Time) ||
		!config.Status.SyncTime.After(config.Spec.DeleteSyncedTime.Time) ||
		len(errs) > 0 ||
		len(config.Status.SyncErrors) > 0 ||
		len(resConditions) > 0 ||
		len(config.Status.ResourceConditions) > 0
}

// setNamespaceConfigStatus updates the status of the given NamespaceConfig.
// initTime is the syncer-controller's instantiation time. It is used to avoid updating
// the sync state and the sync time for the imported stale configs, so that the
// repostatus-reconciler can force-update the stalled configs on startup.
func (r *namespaceConfigReconciler) setNamespaceConfigStatus(ctx context.Context, config *v1.NamespaceConfig, initTime metav1.Time, errs []v1.ConfigManagementError, rcs []v1.ResourceCondition) status.Error {
	if !needsUpdate(config, initTime, errs, rcs) {
		klog.V(3).Infof("Skipping status update for NamespaceConfig %q; status is already up-to-date.", config.Name)
		return nil
	}

	updateFn := func(obj client.Object) (client.Object, error) {
		newPN := obj.(*v1.NamespaceConfig)
		newPN.Status.Token = config.Spec.Token
		newPN.Status.SyncTime = r.now()
		newPN.Status.ResourceConditions = rcs
		newPN.Status.SyncErrors = errs
		if len(errs) > 0 {
			newPN.Status.SyncState = v1.StateError
		} else {
			newPN.Status.SyncState = v1.StateSynced
		}

		newPN.SetGroupVersionKind(kinds.NamespaceConfig())
		return newPN, nil
	}
	_, err := r.client.UpdateStatus(ctx, config, updateFn)
	// TODO: Missing error monitoring like util.go/SetClusterConfigStatus.
	return err
}

// newSyncError returns a ConfigManagementError corresponding to the given NamespaceConfig and error
func newSyncError(config *v1.NamespaceConfig, err error) v1.ConfigManagementError {
	e := v1.ErrorResource{
		SourcePath:        config.GetAnnotations()[metadata.SourcePathAnnotationKey],
		ResourceName:      config.GetName(),
		ResourceNamespace: config.GetNamespace(),
		ResourceGVK:       config.GroupVersionKind(),
	}
	cme := v1.ConfigManagementError{
		ErrorMessage: err.Error(),
	}
	cme.ErrorResources = append(cme.ErrorResources, e)
	return cme
}

func asNamespace(namespaceConfig *v1.NamespaceConfig) *corev1.Namespace {
	namespace := &corev1.Namespace{}
	namespace.Name = namespaceConfig.Name
	namespace.SetGroupVersionKind(kinds.Namespace())

	for k, v := range namespaceConfig.Labels {
		core.SetLabel(namespace, k, v)
	}

	for k, v := range namespaceConfig.Annotations {
		core.SetAnnotation(namespace, k, v)
	}
	enableManagement(namespace)
	core.SetAnnotation(namespace, metadata.SyncTokenAnnotationKey, namespaceConfig.Spec.Token)
	return namespace
}

func (r *namespaceConfigReconciler) createNamespace(ctx context.Context, namespaceConfig *v1.NamespaceConfig) error {
	klog.V(2).Infof("Creating namespace %q based upon NamespaceConfig declaration.", namespaceConfig.Name)
	namespace := asNamespace(namespaceConfig)
	u, err := AsUnstructuredSanitized(namespace)
	if err == nil {
		_, err = r.applier.Create(ctx, u)
	}

	if err != nil {
		r.recorder.Eventf(namespaceConfig, corev1.EventTypeWarning, v1.EventReasonNamespaceCreateFailed,
			"failed to create namespace: %q", err)
		return errors.Wrapf(err, "failed to create namespace %q", namespaceConfig.Name)
	}
	return nil
}

func (r *namespaceConfigReconciler) updateNamespace(ctx context.Context, namespaceConfig *v1.NamespaceConfig, actual *corev1.Namespace) error {
	klog.V(2).Infof("Updating namespace %q based upon NamespaceConfig declaration.", namespaceConfig.Name)
	intended := asNamespace(namespaceConfig)

	uActual, err := AsUnstructuredSanitized(actual)
	if err != nil {
		return err
	}
	uIntended, err := AsUnstructuredSanitized(intended)
	if err != nil {
		return err
	}

	_, err = r.applier.Update(ctx, uIntended, uActual)

	if err != nil {
		r.recorder.Eventf(namespaceConfig, corev1.EventTypeWarning, v1.EventReasonNamespaceUpdateFailed,
			"failed to update namespace: %q", err)
		return errors.Wrapf(err, "failed to update namespace %q", namespaceConfig.Name)
	}
	return nil
}

func (r *namespaceConfigReconciler) deleteNamespace(ctx context.Context, namespace *corev1.Namespace) error {
	klog.V(2).Infof("Deleting namespace %q because it is marked for deletion in corresponding NamespaceConfig.", namespace.GetName())

	err := r.client.Delete(ctx, namespace)

	// Synchronous delete failure
	if err != nil {
		wrapErr := errors.Wrapf(err, "failed to delete namespace %q", namespace.GetName())
		return wrapErr
	}

	return nil
}

func (r *namespaceConfigReconciler) deleteNsConfig(ctx context.Context, config *v1.NamespaceConfig) error {
	if config == nil {
		// We should never reach this code path.
		klog.Warningf("Attempted to delete nonexistent NamespaceConfig")
		return nil
	}
	klog.V(2).Infof("Namespace %q was removed, deleting corresponding NamespaceConfig", config.GetName())

	if differ.ManagementDisabled(config) {
		klog.Infof("Namespace %q is management disabled, deleting corresponding NamespaceConfig", config.GetName())
	} else {
		ns := &corev1.Namespace{}
		err := r.client.Get(ctx, apitypes.NamespacedName{Name: config.Name}, ns)
		if err == nil {
			// We were unexpectedly able to retrieve the namespace
			return errors.Errorf("Namespace %s was found even though it should have been deleted. Namespace: %v", config.GetName(), ns)
		}

		if !apierrors.IsNotFound(err) {
			return errors.Wrapf(err, "Unexpected error retrieving Namespace %s after deletion", config.GetName())
		}
	}

	if err := r.client.Delete(ctx, config); err != nil {
		return errors.Wrapf(err, "Error deleting NamespaceConfig %s", config.GetName())
	}

	return nil
}

// deleteManageableSystem handles the special case of "deleting" manageable system namespaces:
// it does not remove the namespace itself as that is not allowed.
// Instead, it manages all configs inside as if the namespace has no managed resources, and then
// removes the corresponding NamespaceConfig
func (r *namespaceConfigReconciler) deleteManageableSystem(ctx context.Context, ns *corev1.Namespace, config *v1.NamespaceConfig, syncErrs []v1.ConfigManagementError) error {
	if err := r.manageConfigs(ctx, ns.GetName(), reservedNamespaceConfig, syncErrs); err != nil {
		return err
	}

	// Remove the metadata from the namespace only after the resources
	// inside have been processed.
	if err := r.unmanageNamespace(ctx, ns); err != nil {
		return err
	}

	if config == nil {
		// We should never reach this code path.
		klog.Warningf("Attempted to delete nonexistent NamespaceConfig")
		return nil
	}
	klog.V(2).Infof("System Namespace %q was unmanaged; deleting corresponding NamespaceConfig", config.GetName())

	if err := r.client.Delete(ctx, config); err != nil {
		return errors.Wrapf(err, "Error deleting NamespaceConfig %s after unmanaging namespace", config.GetName())
	}
	return nil
}
