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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
	"kpt.dev/configsync/pkg/util/compare"
	"kpt.dev/configsync/pkg/validate/raw/validate"
	kstatus "sigs.k8s.io/cli-utils/pkg/kstatus/status"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ReconcilerType defines the type of a reconciler
type ReconcilerType string

const (
	// RootReconcilerType defines the type for a root reconciler
	RootReconcilerType = ReconcilerType("root")
	// NamespaceReconcilerType defines the type for a namespace reconciler
	NamespaceReconcilerType = ReconcilerType("namespace")
)

// RootSyncReconciler reconciles a RootSync object
type RootSyncReconciler struct {
	reconcilerBase

	lock sync.Mutex
}

// NewRootSyncReconciler returns a new RootSyncReconciler.
func NewRootSyncReconciler(clusterName string, reconcilerPollingPeriod, hydrationPollingPeriod, helmSyncVersionPollingPeriod time.Duration, client client.Client, watcher client.WithWatch, dynamicClient dynamic.Interface, log logr.Logger, scheme *runtime.Scheme) *RootSyncReconciler {
	return &RootSyncReconciler{
		reconcilerBase: reconcilerBase{
			loggingController: loggingController{
				log: log,
			},
			clusterName:                  clusterName,
			client:                       client,
			watcher:                      watcher,
			dynamicClient:                dynamicClient,
			scheme:                       scheme,
			reconcilerPollingPeriod:      reconcilerPollingPeriod,
			hydrationPollingPeriod:       hydrationPollingPeriod,
			helmSyncVersionPollingPeriod: helmSyncVersionPollingPeriod,
			syncKind:                     configsync.RootSyncKind,
		},
	}
}

// +kubebuilder:rbac:groups=configsync.gke.io,resources=rootsyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=configsync.gke.io,resources=rootsyncs/status,verbs=get;update;patch

// Reconcile the RootSync resource.
func (r *RootSyncReconciler) Reconcile(ctx context.Context, req controllerruntime.Request) (controllerruntime.Result, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	rsRef := req.NamespacedName
	start := time.Now()
	reconcilerRef := types.NamespacedName{
		Namespace: configmanagement.ControllerNamespace,
		Name:      core.RootReconcilerName(rsRef.Name),
	}
	ctx = r.setLoggerValues(ctx,
		logFieldSyncKind, r.syncKind,
		logFieldSyncRef, rsRef.String(),
		logFieldReconciler, reconcilerRef.String())
	rs := &v1beta1.RootSync{}

	if err := r.client.Get(ctx, rsRef, rs); err != nil {
		if apierrors.IsNotFound(err) {
			// Cleanup after already deleted RootSync.
			// This code path is unlikely, because the custom finalizer should
			// have already deleted the managed resources and removed the
			// rootSyncs cache entry. But if we get here, clean up anyway.
			if err := r.deleteManagedObjects(ctx, reconcilerRef); err != nil {
				r.logger(ctx).Error(err, "Failed to delete managed objects")
				// Failed to delete a managed object.
				// Return an error to trigger retry.
				metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
				// requeue for retry
				return controllerruntime.Result{}, errors.Wrap(err, "failed to delete managed objects")
			}
			// cleanup successful
			metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(nil), start)
			return controllerruntime.Result{}, nil
		}
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, status.APIServerError(err, "failed to get RootSync")
	}

	if !rs.DeletionTimestamp.IsZero() {
		// Deletion requested.
		// Cleanup is handled above, after the RootSync is NotFound.
		r.logger(ctx).V(3).Info("Sync deletion timestamp detected")
	}

	currentRS := rs.DeepCopy()

	if err := r.validateNamespaceName(rsRef.Namespace); err != nil {
		r.logger(ctx).Error(err, "Sync namespace invalid")
		rootsync.SetStalled(rs, "Validation", err)
		// Validation errors should not trigger retry (return error),
		// unless the status update also fails.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		// Use the validation error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, updateErr
	}

	if errs := validation.IsDNS1123Subdomain(reconcilerRef.Name); errs != nil {
		err := errors.Errorf("Invalid reconciler name %q: %s.", reconcilerRef.Name, strings.Join(errs, ", "))
		r.logger(ctx).Error(err, "Sync name or namespace invalid")
		rootsync.SetStalled(rs, "Validation", err)
		// Validation errors should not trigger retry (return error),
		// unless the status update also fails.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		// Use the validation error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, updateErr
	}

	if err := r.validateSpec(ctx, rs, reconcilerRef.Name); err != nil {
		r.logger(ctx).Error(err, "Sync spec invalid")
		rootsync.SetStalled(rs, "Validation", err)
		// Validation errors should not trigger retry (return error),
		// unless the status update also fails.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		// Use the validation error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, updateErr
	}

	setupFn := func(ctx context.Context) error {
		return r.setup(ctx, reconcilerRef, rsRef, currentRS, rs)
	}
	teardownFn := func(ctx context.Context) error {
		return r.teardown(ctx, reconcilerRef, rsRef, currentRS, rs)
	}

	if err := r.setupOrTeardown(ctx, rs, setupFn, teardownFn); err != nil {
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, err
	}

	metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(nil), start)
	return controllerruntime.Result{}, nil
}

func (r *RootSyncReconciler) upsertManagedObjects(ctx context.Context, reconcilerRef types.NamespacedName, rs *v1beta1.RootSync) error {
	r.logger(ctx).V(3).Info("Reconciling managed objects")

	// Note: RootSync Secret is managed by the user, not the ReconcilerManager.
	// This is because it's in the same namespace as the Deployment, so we don't
	// need to copy it to the config-management-system namespace.

	labelMap := map[string]string{
		metadata.SyncNamespaceLabel: rs.Namespace,
		metadata.SyncNameLabel:      rs.Name,
		metadata.SyncKindLabel:      r.syncKind,
	}

	// Overwrite reconciler pod ServiceAccount.
	var auth configsync.AuthType
	var gcpSAEmail string
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		auth = rs.Spec.Auth
		gcpSAEmail = rs.Spec.GCPServiceAccountEmail
	case v1beta1.OciSource:
		auth = rs.Spec.Oci.Auth
		gcpSAEmail = rs.Spec.Oci.GCPServiceAccountEmail
	case v1beta1.HelmSource:
		auth = rs.Spec.Helm.Auth
		gcpSAEmail = rs.Spec.Helm.GCPServiceAccountEmail
	default:
		// Should have been caught by validation
		return errors.Errorf("invalid source type: %s", rs.Spec.SourceType)
	}
	if _, err := r.upsertServiceAccount(ctx, reconcilerRef, auth, gcpSAEmail, labelMap); err != nil {
		return err
	}

	// Overwrite reconciler clusterrolebinding.
	if _, err := r.upsertClusterRoleBinding(ctx, reconcilerRef); err != nil {
		return err
	}

	containerEnvs := r.populateContainerEnvs(ctx, rs, reconcilerRef.Name)
	mut := r.mutationsFor(ctx, rs, containerEnvs)

	// Upsert Root reconciler deployment.
	deployObj, op, err := r.upsertDeployment(ctx, reconcilerRef, labelMap, mut)
	if err != nil {
		return err
	}
	rs.Status.Reconciler = reconcilerRef.Name

	// Get the latest deployment to check the status.
	// For other operations, upsertDeployment will have returned the latest already.
	if op == controllerutil.OperationResultNone {
		deployObj, err = r.deployment(ctx, reconcilerRef)
		if err != nil {
			return err
		}
	}

	gvk, err := kinds.Lookup(deployObj, r.scheme)
	if err != nil {
		return err
	}
	deployID := core.ID{
		ObjectKey: reconcilerRef,
		GroupKind: gvk.GroupKind(),
	}

	result, err := kstatus.Compute(deployObj)
	if err != nil {
		return errors.Wrap(err, "computing reconciler Deployment status failed")
	}

	r.logger(ctx).V(3).Info("Reconciler status",
		logFieldObjectRef, deployID.ObjectKey.String(),
		logFieldObjectKind, deployID.Kind,
		logFieldResourceVersion, deployObj.GetResourceVersion(),
		"status", result.Status,
		"message", result.Message)

	if result.Status != kstatus.CurrentStatus {
		// reconciler deployment failed or not yet available
		err := errors.New(result.Message)
		return NewObjectReconcileErrorWithID(err, deployID, result.Status)
	}

	// success - reconciler deployment is available
	return nil
}

// setup performs the following steps:
// - Create or update managed objects
// - Convert any error into RootSync status conditions
// - Update the RootSync status
func (r *RootSyncReconciler) setup(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, currentRS, rs *v1beta1.RootSync) error {
	err := r.upsertManagedObjects(ctx, reconcilerRef, rs)
	err = r.handleReconcileError(ctx, err, rs, "Setup")
	updated, updateErr := r.updateStatus(ctx, currentRS, rs)
	if updateErr != nil {
		if err == nil {
			// If no other errors, return the updateStatus error and retry
			return updateErr
		}
		// else - log the updateStatus error and retry
		r.logger(ctx).Error(updateErr, "Object status update failed",
			logFieldObjectRef, rsRef.String(),
			logFieldObjectKind, r.syncKind)
		return err
	}
	if err != nil {
		return err
	}
	if updated {
		r.logger(ctx).Info("Setup successful")
	}
	return nil
}

// teardown performs the following steps:
// - Delete managed objects
// - Convert any error into RootSync status conditions
// - Update the RootSync status
func (r *RootSyncReconciler) teardown(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, currentRS, rs *v1beta1.RootSync) error {
	err := r.deleteManagedObjects(ctx, reconcilerRef)
	err = r.handleReconcileError(ctx, err, rs, "Teardown")
	updated, updateErr := r.updateStatus(ctx, currentRS, rs)
	if updateErr != nil {
		if err == nil {
			// If no other errors, return the updateStatus error and retry
			return updateErr
		}
		// else - log the updateStatus error and retry
		r.logger(ctx).Error(updateErr, "Object status update failed",
			logFieldObjectRef, rsRef.String(),
			logFieldObjectKind, r.syncKind)
		return err
	}
	if err != nil {
		return err
	}
	if updated {
		r.logger(ctx).Info("Teardown successful")
	}
	return nil
}

// handleReconcileError updates the sync object status to reflect the Reconcile
// error. If the error requires immediate retry, it will be returned.
func (r *RootSyncReconciler) handleReconcileError(ctx context.Context, err error, rs *v1beta1.RootSync, stage string) error {
	if err == nil {
		rootsync.ClearCondition(rs, v1beta1.RootSyncReconciling)
		rootsync.ClearCondition(rs, v1beta1.RootSyncStalled)
		return nil // no retry
	}

	// Most errors are either ObjectOperationError or ObjectReconcileError.
	// The type of error indicates whether setup/teardown is stalled or still making progress (waiting for next event).
	var opErr *ObjectOperationError
	var statusErr *ObjectReconcileError
	if errors.As(err, &opErr) {
		// Metadata from ManagedObjectOperationError used for log context
		r.logger(ctx).Error(err, fmt.Sprintf("%s failed", stage),
			logFieldObjectRef, opErr.ID.ObjectKey.String(),
			logFieldObjectKind, opErr.ID.Kind,
			logFieldOperation, opErr.Operation)
		rootsync.SetReconciling(rs, stage, fmt.Sprintf("%s stalled", stage))
		rootsync.SetStalled(rs, opErr.ID.Kind, err)
	} else if errors.As(err, &statusErr) {
		// Metadata from ObjectReconcileError used for log context
		r.logger(ctx).Error(err, fmt.Sprintf("%s waiting for event", stage),
			logFieldObjectRef, statusErr.ID.ObjectKey.String(),
			logFieldObjectKind, statusErr.ID.Kind,
			logFieldObjectStatus, statusErr.Status)
		switch statusErr.Status {
		case kstatus.InProgressStatus, kstatus.TerminatingStatus:
			// still making progress
			rootsync.SetReconciling(rs, statusErr.ID.Kind, err.Error())
			rootsync.ClearCondition(rs, v1beta1.RootSyncStalled)
			return nil // no retry - wait for next event
		default:
			// failed or invalid
			rootsync.SetReconciling(rs, stage, fmt.Sprintf("%s stalled", stage))
			rootsync.SetStalled(rs, statusErr.ID.Kind, err)
		}
	} else {
		r.logger(ctx).Error(err, fmt.Sprintf("%s failed", stage))
		rootsync.SetReconciling(rs, stage, fmt.Sprintf("%s stalled", stage))
		rootsync.SetStalled(rs, "Error", err)
	}
	return err // retry
}

// deleteManagedObjects deletes objects managed by the reconciler-manager for
// this RootSync. If updateStatus is true, the RootSync status will be updated
// to reflect the teardown progress.
func (r *RootSyncReconciler) deleteManagedObjects(ctx context.Context, reconcilerRef types.NamespacedName) error {
	r.logger(ctx).Info("Deleting managed objects")

	if err := r.deleteDeployment(ctx, reconcilerRef); err != nil {
		return err
	}

	// Note: ConfigMaps have been replaced by Deployment env vars.
	// Using env vars auto-updates the Deployment when they change.
	// This deletion remains to clean up after users upgrade.

	if err := r.deleteConfigMaps(ctx, reconcilerRef); err != nil {
		return err
	}

	// Note: ReconcilerManager doesn't manage the RootSync Secret.
	// So we don't need to delete it here.

	if err := r.deleteClusterRoleBinding(ctx, reconcilerRef); err != nil {
		return err
	}

	return r.deleteServiceAccount(ctx, reconcilerRef)
}

// SetupWithManager registers RootSync controller with reconciler-manager.
func (r *RootSyncReconciler) SetupWithManager(mgr controllerruntime.Manager, watchFleetMembership bool) error {
	// Index the `gitSecretRefName` field, so that we will be able to lookup RootSync be a referenced `SecretRef` name.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1beta1.RootSync{}, gitSecretRefField, func(rawObj client.Object) []string {
		rs, ok := rawObj.(*v1beta1.RootSync)
		if !ok {
			// Only add index for RootSync
			return nil
		}
		if rs.Spec.Git == nil || v1beta1.GetSecretName(rs.Spec.Git.SecretRef) == "" {
			return nil
		}
		return []string{rs.Spec.Git.SecretRef.Name}
	}); err != nil {
		return err
	}

	controllerBuilder := controllerruntime.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		For(&v1beta1.RootSync{}).
		// Custom Watch to trigger Reconcile for objects created by RootSync controller.
		Watches(&source.Kind{Type: withNamespace(&corev1.Secret{}, configsync.ControllerNamespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToRootSyncs),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&source.Kind{Type: withNamespace(&corev1.ConfigMap{}, configsync.ControllerNamespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigMapsToRootSyncs),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&source.Kind{Type: withNamespace(&appsv1.Deployment{}, configsync.ControllerNamespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectToRootSync),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&source.Kind{Type: withNamespace(&corev1.ServiceAccount{}, configsync.ControllerNamespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectToRootSync),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&source.Kind{Type: &rbacv1.ClusterRoleBinding{}},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectToRootSync),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}))

	if watchFleetMembership {
		// Custom Watch for membership to trigger reconciliation.
		controllerBuilder.Watches(&source.Kind{Type: &hubv1.Membership{}},
			handler.EnqueueRequestsFromMapFunc(r.mapMembershipToRootSyncs()),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	}
	return controllerBuilder.Complete(r)
}

func withNamespace(obj client.Object, ns string) client.Object {
	obj.SetNamespace(ns)
	return obj
}

func (r *RootSyncReconciler) mapMembershipToRootSyncs() func(client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		// Clear the membership if the cluster is unregistered
		if err := r.client.Get(context.Background(), types.NamespacedName{Name: fleetMembershipName}, &hubv1.Membership{}); err != nil {
			if apierrors.IsNotFound(err) {
				klog.Info("Fleet Membership not found, clearing membership cache")
				r.membership = nil
				return r.requeueAllRootSyncs()
			}
			klog.Errorf("Fleet Membership get failed: %v", err)
			return nil
		}

		m, isMembership := o.(*hubv1.Membership)
		if !isMembership {
			klog.Errorf("Fleet Membership expected, found %q", o.GetObjectKind().GroupVersionKind())
			return nil
		}
		if m.Name != fleetMembershipName {
			klog.Errorf("Fleet Membership name expected %q, found %q", fleetMembershipName, m.Name)
			return nil
		}
		r.membership = m
		return r.requeueAllRootSyncs()
	}
}

// mapConfigMapsToRootSyncs handles updates to referenced ConfigMaps
func (r *RootSyncReconciler) mapConfigMapsToRootSyncs(obj client.Object) []reconcile.Request {
	objRef := client.ObjectKeyFromObject(obj)

	// Ignore changes from other namespaces.
	if objRef.Namespace != configsync.ControllerNamespace {
		return nil
	}

	ctx := context.Background()
	allRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, allRootSyncs); err != nil {
		klog.Error("failed to list all RootSyncs for %s (%s): %v",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, err)
		return nil
	}

	var result []reconcile.Request
	for _, rs := range allRootSyncs.Items {
		if rs.Spec.SourceType != string(v1beta1.HelmSource) {
			// we are only watching ConfigMaps to retrigger helm-sync, so we can ignore
			// other source types
			continue
		}
		if rs.Spec.Helm == nil || len(rs.Spec.Helm.ValuesFileSources) == 0 {
			continue
		}

		var referenced bool
		var key string
		for _, vf := range rs.Spec.Helm.ValuesFileSources {
			if vf.Name == objRef.Name {
				referenced = true
				key = vf.ValuesFile
				break
			}
		}

		if !referenced {
			continue
		}

		if err := validate.CheckConfigMapKeyExists(ctx, r.client, objRef, key, &rs); err != nil {
			// If the ConfigMap was deleted or it no longer has the specified data key, we skip restarting the pod
			// to avoid losing or modifying already synced resources
			klog.Errorf("ConfigMap ref validation error: %s", err.Error())
			// requeue the reconcile to rerun validation checks and status setting
			result = append(result, requeueRootSyncRequest(obj, &rs)...)

		}

		rsRef := client.ObjectKeyFromObject(&rs)
		reconcilerRef := types.NamespacedName{
			Namespace: configmanagement.ControllerNamespace,
			Name:      core.RootReconcilerName(rsRef.Name),
		}

		var reconciler appsv1.Deployment
		cl := r.reconcilerBase.client
		if err := cl.Get(ctx, reconcilerRef, &reconciler); err != nil {
			klog.Errorf("failed to get reconciler reference: %s", err.Error())
			if apierrors.IsNotFound(err) {
				// if the root-reconciler doesn't exist, an updated ConfigMap should trigger a reconcile
				// to create one
				result = append(result, requeueRootSyncRequest(obj, &rs)...)
			}
			continue
		}

		// A rereconcile doesn't necessarily update an existing root-reconciler deployment, so
		// the root-reconciler pods need to be explicitly restarted to pick up the ConfigMap update.
		reconciler.Spec.Template.ObjectMeta.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)
		if err := cl.Update(ctx, &reconciler); err != nil {
			klog.Errorf("failed to update reconciler: %s", err.Error())
			return nil
		}

		klog.Infof("Changes to ConfigMap %s triggered restart of pods in Deployment %s\n", objRef, reconcilerRef)
		result = append(result, requeueRootSyncRequest(obj, &rs)...)

	}
	return result
}

// mapObjectToRootSync define a mapping from an object in 'config-management-system'
// namespace to a RootSync to be reconciled.
func (r *RootSyncReconciler) mapObjectToRootSync(obj client.Object) []reconcile.Request {
	objRef := client.ObjectKeyFromObject(obj)

	// Ignore changes from other namespaces because all the generated resources
	// exist in the config-management-system namespace.
	// Allow the empty namespace for ClusterRoleBindings.
	if objRef.Namespace != "" && objRef.Namespace != configsync.ControllerNamespace {
		return nil
	}

	// Ignore changes from resources without the root-reconciler prefix or configsync.gke.io:root-reconciler
	// because all the generated resources have the prefix.
	roleBindingName := RootSyncPermissionsName()
	if !strings.HasPrefix(objRef.Name, core.RootReconcilerPrefix) && objRef.Name != roleBindingName {
		return nil
	}

	if err := r.addTypeInformationToObject(obj); err != nil {
		klog.Errorf("failed to lookup resource of object %T (%s): %v",
			obj, objRef, err)
		return nil
	}

	allRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(context.Background(), allRootSyncs); err != nil {
		klog.Error("failed to list all RootSyncs for %s (%s): %v",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, err)
		return nil
	}

	// Most of the resources are mapped to a single RootSync object except ClusterRoleBinding.
	// All RootSync objects share the same ClusterRoleBinding object, so requeue all RootSync objects if the ClusterRoleBinding is changed.
	// For other resources, requeue the mapping RootSync object and then return.
	var requests []reconcile.Request
	var attachedRSNames []string
	for _, rs := range allRootSyncs.Items {
		reconcilerName := core.RootReconcilerName(rs.GetName())
		switch obj.(type) {
		case *rbacv1.ClusterRoleBinding:
			if objRef.Name == roleBindingName {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&rs)})
				attachedRSNames = append(attachedRSNames, rs.GetName())
			}
		default: // Deployment and ServiceAccount
			if objRef.Name == reconcilerName {
				return requeueRootSyncRequest(obj, &rs)
			}
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to %s (%s) triggers a reconciliation for the RootSync(s) (%s)",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, strings.Join(attachedRSNames, ", "))
	}
	return requests
}

func requeueRootSyncRequest(obj client.Object, rs *v1beta1.RootSync) []reconcile.Request {
	rsRef := client.ObjectKeyFromObject(rs)
	klog.Infof("Changes to %s (%s) triggers a reconciliation for the RootSync (%s)",
		obj.GetObjectKind().GroupVersionKind().Kind, client.ObjectKeyFromObject(obj), rsRef)
	return []reconcile.Request{
		{
			NamespacedName: rsRef,
		},
	}
}

func (r *RootSyncReconciler) requeueAllRootSyncs() []reconcile.Request {
	allRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(context.Background(), allRootSyncs); err != nil {
		klog.Errorf("RootSync list failed: %v", err)
		return nil
	}

	requests := make([]reconcile.Request, len(allRootSyncs.Items))
	for i, rs := range allRootSyncs.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&rs),
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to membership trigger reconciliations for %d RootSync objects.", len(allRootSyncs.Items))
	}
	return requests
}

// mapSecretToRootSyncs define a mapping from the Secret object to its attached
// RootSync objects via the `spec.git.secretRef.name` field .
// The update to the Secret object will trigger a reconciliation of the RootSync objects.
func (r *RootSyncReconciler) mapSecretToRootSyncs(secret client.Object) []reconcile.Request {
	sRef := client.ObjectKeyFromObject(secret)
	// Ignore secret in other namespaces because the RootSync's git secret MUST
	// exist in the config-management-system namespace.
	if sRef.Namespace != configsync.ControllerNamespace {
		return nil
	}

	// Ignore secret starts with ns-reconciler prefix because those are for RepoSync objects.
	if strings.HasPrefix(sRef.Name, core.NsReconcilerPrefix+"-") {
		return nil
	}

	attachedRootSyncs := &v1beta1.RootSyncList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gitSecretRefField, sRef.Name),
		Namespace:     sRef.Namespace,
	}
	if err := r.client.List(context.Background(), attachedRootSyncs, listOps); err != nil {
		klog.Errorf("RootSync list failed for Secret (%s): %v", sRef, err)
		return nil
	}

	requests := make([]reconcile.Request, len(attachedRootSyncs.Items))
	attachedRSNames := make([]string, len(attachedRootSyncs.Items))
	for i, rs := range attachedRootSyncs.Items {
		attachedRSNames[i] = rs.GetName()
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&rs),
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to Secret (%s) triggers a reconciliation for the RootSync objects: %s",
			sRef, strings.Join(attachedRSNames, ", "))
	}
	return requests
}

func (r *RootSyncReconciler) populateContainerEnvs(ctx context.Context, rs *v1beta1.RootSync, reconcilerName string) map[string][]corev1.EnvVar {
	result := map[string][]corev1.EnvVar{
		reconcilermanager.HydrationController: hydrationEnvs(rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, declared.RootReconciler, reconcilerName, r.hydrationPollingPeriod.String()),
		reconcilermanager.Reconciler:          append(reconcilerEnvs(r.clusterName, rs.Name, reconcilerName, declared.RootReconciler, rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, rootsync.GetHelmBase(rs.Spec.Helm), r.reconcilerPollingPeriod.String(), rs.Spec.SafeOverride().StatusMode, v1beta1.GetReconcileTimeout(rs.Spec.SafeOverride().ReconcileTimeout), v1beta1.GetAPIServerTimeout(rs.Spec.SafeOverride().APIServerTimeout), enableRendering(rs.GetAnnotations())), sourceFormatEnv(rs.Spec.SourceFormat)),
	}
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		result[reconcilermanager.GitSync] = gitSyncEnvs(ctx, options{
			ref:             rs.Spec.Git.Revision,
			branch:          rs.Spec.Git.Branch,
			repo:            rs.Spec.Git.Repo,
			secretType:      rs.Spec.Git.Auth,
			period:          v1beta1.GetPeriodSecs(rs.Spec.Git.Period),
			proxy:           rs.Spec.Proxy,
			depth:           rs.Spec.SafeOverride().GitSyncDepth,
			noSSLVerify:     rs.Spec.Git.NoSSLVerify,
			caCertSecretRef: v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef),
		})
	case v1beta1.OciSource:
		result[reconcilermanager.OciSync] = ociSyncEnvs(rs.Spec.Oci.Image, rs.Spec.Oci.Auth, v1beta1.GetPeriodSecs(rs.Spec.Oci.Period))
	case v1beta1.HelmSource:
		result[reconcilermanager.HelmSync] = helmSyncEnvs(&rs.Spec.Helm.HelmBase, rs.Spec.Helm.Namespace, rs.Spec.Helm.DeployNamespace, r.helmSyncVersionPollingPeriod.String())
	}
	return result
}

func (r *RootSyncReconciler) validateSpec(ctx context.Context, rs *v1beta1.RootSync, reconcilerName string) error {
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		return r.validateGitSpec(ctx, rs, reconcilerName)
	case v1beta1.OciSource:
		return validate.OciSpec(rs.Spec.Oci, rs)
	case v1beta1.HelmSource:
		if err := validate.HelmSpec(rootsync.GetHelmBase(rs.Spec.Helm), rs); err != nil {
			return err
		}
		if err := validate.CheckValuesFileSourcesRefs(ctx, r.client, rs.Spec.Helm.ValuesFileSources, rs); err != nil {
			return err
		}
		if rs.Spec.Helm.Namespace != "" && rs.Spec.Helm.DeployNamespace != "" {
			return validate.HelmNSAndDeployNS(rs)
		}
		return nil
	default:
		return validate.InvalidSourceType(rs)
	}
}

func (r *RootSyncReconciler) validateGitSpec(ctx context.Context, rs *v1beta1.RootSync, reconcilerName string) error {
	if err := validate.GitSpec(rs.Spec.Git, rs); err != nil {
		return err
	}
	if err := r.validateCACertSecret(ctx, rs.Namespace, v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef)); err != nil {
		return err
	}
	return r.validateRootSecret(ctx, rs, reconcilerName)
}

func (r *RootSyncReconciler) validateNamespaceName(namespaceName string) error {
	if namespaceName != configsync.ControllerNamespace {
		return fmt.Errorf("RootSync objects are only allowed in the %s namespace, not in %s", configsync.ControllerNamespace, namespaceName)
	}
	return nil
}

// validateRootSecret verify that any necessary Secret is present before creating ConfigMaps and Deployments.
func (r *RootSyncReconciler) validateRootSecret(ctx context.Context, rootSync *v1beta1.RootSync, reconcilerName string) error {
	if SkipForAuth(rootSync.Spec.Auth) {
		// There is no Secret to check for the Config object.
		return nil
	}

	secretName := ReconcilerResourceName(reconcilerName, rootSync.Spec.SecretRef.Name)
	if errs := validation.IsDNS1123Label(secretName); errs != nil {
		return errors.Errorf("The managed secret name %q is invalid: %s. To fix it, update '.spec.git.secretRef.name'", secretName, strings.Join(errs, ", "))
	}

	secret, err := validateSecretExist(ctx,
		v1beta1.GetSecretName(rootSync.Spec.SecretRef),
		rootSync.Namespace,
		r.client)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return errors.Errorf("Secret %s not found: create one to allow client authentication", v1beta1.GetSecretName(rootSync.Spec.SecretRef))
		}
		return errors.Wrapf(err, "Secret %s get failed", v1beta1.GetSecretName(rootSync.Spec.SecretRef))
	}
	return validateSecretData(rootSync.Spec.Auth, secret)
}

func (r *RootSyncReconciler) upsertClusterRoleBinding(ctx context.Context, reconcilerRef types.NamespacedName) (client.ObjectKey, error) {
	crbRef := client.ObjectKey{Name: RootSyncPermissionsName()}
	childCRB := &rbacv1.ClusterRoleBinding{}
	childCRB.Name = crbRef.Name

	op, err := CreateOrUpdate(ctx, r.client, childCRB, func() error {
		childCRB.OwnerReferences = nil
		childCRB.RoleRef = rolereference("cluster-admin", "ClusterRole")
		childCRB.Subjects = addSubject(childCRB.Subjects, r.serviceAccountSubject(reconcilerRef))
		// Remove existing OwnerReferences, now that we're using finalizers.
		childCRB.OwnerReferences = nil
		return nil
	})
	if err != nil {
		return crbRef, err
	}
	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, crbRef.String(),
			logFieldObjectKind, "ClusterRoleBinding",
			logFieldOperation, op)
	}
	return crbRef, nil
}

func (r *RootSyncReconciler) updateStatus(ctx context.Context, currentRS, rs *v1beta1.RootSync) (bool, error) {
	rs.Status.ObservedGeneration = rs.Generation

	// Avoid unnecessary status updates.
	if cmp.Equal(currentRS.Status, rs.Status, compare.IgnoreTimestampUpdates) {
		r.logger(ctx).V(3).Info("Skipping sync status update",
			logFieldResourceVersion, rs.ResourceVersion)
		return false, nil
	}

	if r.logger(ctx).V(5).Enabled() {
		r.logger(ctx).Info("Updating sync status",
			logFieldResourceVersion, rs.ResourceVersion,
			"diff", fmt.Sprintf("Diff (- Expected, + Actual):\n%s", cmp.Diff(currentRS.Status, rs.Status)))
	}

	err := r.client.Status().Update(ctx, rs)
	if err != nil {
		return false, err
	}
	return true, nil
}

// enableRendering returns whether rendering should be enabled for the reconciler
// of this RSync. This is determined by an annotation that is set on the RSync
// by the reconciler.
func enableRendering(annotations map[string]string) bool {
	val, ok := annotations[metadata.RequiresRenderingAnnotationKey]
	if !ok { // default to disabling rendering
		return false
	}
	renderingRequired, err := strconv.ParseBool(val)
	if err != nil {
		// This should never happen, as the annotation should always be set to a
		// valid value by the reconciler. Log the error and return the default value.
		klog.Infof("failed to parse %s value to boolean: %s", metadata.RequiresRenderingAnnotationKey, val)
		return false
	}
	return renderingRequired
}

func (r *RootSyncReconciler) mutationsFor(ctx context.Context, rs *v1beta1.RootSync, containerEnvs map[string][]corev1.EnvVar) mutateFn {
	return func(obj client.Object) error {
		d, ok := obj.(*appsv1.Deployment)
		if !ok {
			return errors.Errorf("expected appsv1 Deployment, got: %T", obj)
		}
		// Remove existing OwnerReferences, now that we're using finalizers.
		d.OwnerReferences = nil
		reconcilerName := core.RootReconcilerName(rs.Name)

		// Only inject the FWI credentials when the auth type is gcpserviceaccount and the membership info is available.
		var auth configsync.AuthType
		var gcpSAEmail string
		var secretRefName string
		var caCertSecretRefName string
		switch v1beta1.SourceType(rs.Spec.SourceType) {
		case v1beta1.GitSource:
			auth = rs.Spec.Auth
			gcpSAEmail = rs.Spec.GCPServiceAccountEmail
			secretRefName = v1beta1.GetSecretName(rs.Spec.SecretRef)
			caCertSecretRefName = v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef)
		case v1beta1.OciSource:
			auth = rs.Spec.Oci.Auth
			gcpSAEmail = rs.Spec.Oci.GCPServiceAccountEmail
		case v1beta1.HelmSource:
			auth = rs.Spec.Helm.Auth
			gcpSAEmail = rs.Spec.Helm.GCPServiceAccountEmail
			secretRefName = v1beta1.GetSecretName(rs.Spec.Helm.SecretRef)
		}
		injectFWICreds := useFWIAuth(auth, r.membership)
		if injectFWICreds {
			if err := r.injectFleetWorkloadIdentityCredentials(&d.Spec.Template, gcpSAEmail); err != nil {
				return err
			}
		}

		// Add unique reconciler label
		core.SetLabel(&d.Spec.Template, metadata.ReconcilerLabel, reconcilerName)

		templateSpec := &d.Spec.Template.Spec

		// Update ServiceAccountName.
		templateSpec.ServiceAccountName = reconcilerName
		// The Deployment object fetched from the API server has the field defined.
		// Update DeprecatedServiceAccount to avoid discrepancy in equality check.
		templateSpec.DeprecatedServiceAccount = reconcilerName

		// Mutate secret.secretname to secret reference specified in RootSync CR.
		// Secret reference is the name of the secret used by git-sync or helm-sync container to
		// authenticate with the git or helm repository using the authorization method specified
		// in the RootSync CR.
		templateSpec.Volumes = filterVolumes(templateSpec.Volumes, auth, secretRefName, caCertSecretRefName, rs.Spec.SourceType, r.membership)

		var updatedContainers []corev1.Container

		for _, container := range templateSpec.Containers {
			addContainer := true
			switch container.Name {
			case reconcilermanager.Reconciler:
				container.Env = append(container.Env, containerEnvs[container.Name]...)
				mutateContainerResource(&container, rs.Spec.Override)
			case reconcilermanager.HydrationController:
				if !enableRendering(rs.GetAnnotations()) {
					// if the sync source does not require rendering, omit the hydration controller
					// this minimizes the resource footprint of the reconciler
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					container.Image = updateHydrationControllerImage(container.Image, *rs.Spec.SafeOverride())
					mutateContainerResource(&container, rs.Spec.Override)
				}
			case reconcilermanager.OciSync:
				// Don't add the oci-sync container when sourceType is NOT oci.
				if v1beta1.SourceType(rs.Spec.SourceType) != v1beta1.OciSource {
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					injectFWICredsToContainer(&container, injectFWICreds)
					mutateContainerResource(&container, rs.Spec.Override)
				}
			case reconcilermanager.HelmSync:
				// Don't add the helm-sync container when sourceType is NOT helm.
				if v1beta1.SourceType(rs.Spec.SourceType) != v1beta1.HelmSource {
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					container.VolumeMounts = volumeMounts(rs.Spec.Helm.Auth, "", rs.Spec.SourceType, container.VolumeMounts)
					if authTypeToken(rs.Spec.Helm.Auth) {
						container.Env = append(container.Env, helmSyncTokenAuthEnv(secretRefName)...)
					}
					mountConfigMapValuesFiles(templateSpec, &container, rs.Spec.Helm.ValuesFileSources)
					injectFWICredsToContainer(&container, injectFWICreds)
					mutateContainerResource(&container, rs.Spec.Override)
				}
			case reconcilermanager.GitSync:
				// Don't add the git-sync container when sourceType is NOT git.
				if v1beta1.SourceType(rs.Spec.SourceType) != v1beta1.GitSource {
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					// Don't mount git-creds volume if auth is 'none' or 'gcenode'.
					container.VolumeMounts = volumeMounts(rs.Spec.Auth, caCertSecretRefName, rs.Spec.SourceType, container.VolumeMounts)
					// Update Environment variables for `token` Auth, which
					// passes the credentials as the Username and Password.
					secretName := v1beta1.GetSecretName(rs.Spec.SecretRef)
					if authTypeToken(rs.Spec.Auth) {
						container.Env = append(container.Env, gitSyncTokenAuthEnv(secretName)...)
					}
					sRef := client.ObjectKey{Namespace: rs.Namespace, Name: secretName}
					keys := GetSecretKeys(ctx, r.client, sRef)
					container.Env = append(container.Env, gitSyncHTTPSProxyEnv(secretName, keys)...)
					mutateContainerResource(&container, rs.Spec.Override)
				}
			case metrics.OtelAgentName:
				// The no-op case to avoid unknown container error after
				// first-ever reconcile.
			default:
				return errors.Errorf("unknown container in reconciler deployment template: %q", container.Name)
			}
			if addContainer {
				updatedContainers = append(updatedContainers, container)
			}
		}

		// Add container spec for the "gcenode-askpass-sidecar" (defined as
		// a constant) to the reconciler Deployment when `.spec.sourceType` is `git`,
		// and `.spec.git.auth` is either `gcenode` or `gcpserviceaccount`.
		// The container is added first time when the reconciler deployment is created.
		if v1beta1.SourceType(rs.Spec.SourceType) == v1beta1.GitSource &&
			(auth == configsync.AuthGCPServiceAccount || auth == configsync.AuthGCENode) {
			updatedContainers = append(updatedContainers, gceNodeAskPassSidecar(gcpSAEmail, injectFWICreds))
		}

		templateSpec.Containers = updatedContainers
		return nil
	}
}
