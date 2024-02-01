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
	"golang.org/x/exp/slices"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
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
	"kpt.dev/configsync/pkg/util/mutate"
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
func NewRootSyncReconciler(clusterName string, reconcilerPollingPeriod, hydrationPollingPeriod time.Duration, client client.Client, watcher client.WithWatch, dynamicClient dynamic.Interface, log logr.Logger, scheme *runtime.Scheme) *RootSyncReconciler {
	return &RootSyncReconciler{
		reconcilerBase: reconcilerBase{
			loggingController: loggingController{
				log: log,
			},
			clusterName:             clusterName,
			client:                  client,
			watcher:                 watcher,
			dynamicClient:           dynamicClient,
			scheme:                  scheme,
			reconcilerPollingPeriod: reconcilerPollingPeriod,
			hydrationPollingPeriod:  hydrationPollingPeriod,
			syncKind:                configsync.RootSyncKind,
			knownHostExist:          false,
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
	reconcilerRef := core.RootReconcilerObjectKey(rsRef.Name)
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
			if err := r.deleteManagedObjects(ctx, reconcilerRef, rsRef); err != nil {
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

	if rs.DeletionTimestamp.IsZero() {
		// Only validate RootSync if it is not deleting. Otherwise, the validation
		// error will block the finalizer.
		if err := r.validateRootSync(ctx, rs, reconcilerRef.Name); err != nil {
			r.logger(ctx).Error(err, "RootSync spec invalid")
			_, updateErr := r.updateSyncStatus(ctx, rs, reconcilerRef, func(syncObj *v1beta1.RootSync) error {
				rootsync.SetStalled(rs, "Validation", err)
				return nil
			})
			// Use the validation error for metric tagging.
			metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
			// Spec errors are not recoverable without user input.
			// So only retry if there was an updateErr.
			return controllerruntime.Result{}, updateErr
		}
	} else {
		r.logger(ctx).V(3).Info("Sync deletion timestamp detected")
	}

	setupFn := func(ctx context.Context) error {
		return r.setup(ctx, reconcilerRef, rs)
	}
	teardownFn := func(ctx context.Context) error {
		return r.teardown(ctx, reconcilerRef, rs)
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

	rsRef := client.ObjectKeyFromObject(rs)
	labelMap := ManagedObjectLabelMap(r.syncKind, rsRef)

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
		return errors.Wrap(err, "upserting service account")
	}

	// Reconcile reconciler RBAC bindings.
	if err := r.manageRBACBindings(ctx, reconcilerRef, rsRef, rs.Spec.SafeOverride().RoleRefs); err != nil {
		return errors.Wrap(err, "configuring RBAC bindings")
	}

	containerEnvs := r.populateContainerEnvs(ctx, rs, reconcilerRef.Name)
	mut := r.mutationsFor(ctx, rs, containerEnvs)

	// Upsert Root reconciler deployment.
	deployObj, op, err := r.upsertDeployment(ctx, reconcilerRef, labelMap, mut)
	if err != nil {
		return errors.Wrap(err, "upserting reconciler deployment")
	}

	// Get the latest deployment to check the status.
	// For other operations, upsertDeployment will have returned the latest already.
	if op == controllerutil.OperationResultNone {
		deployObj, err = r.deployment(ctx, reconcilerRef)
		if err != nil {
			return errors.Wrap(err, "getting reconciler deployment")
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
		return errors.Wrap(err, "computing reconciler deployment status")
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
func (r *RootSyncReconciler) setup(ctx context.Context, reconcilerRef types.NamespacedName, rs *v1beta1.RootSync) error {
	err := r.upsertManagedObjects(ctx, reconcilerRef, rs)
	updated, updateErr := r.updateSyncStatus(ctx, rs, reconcilerRef, func(syncObj *v1beta1.RootSync) error {
		// Modify the sync status,
		// but keep the upsert error separate from the status update error.
		err = r.handleReconcileError(ctx, err, syncObj, "Setup")
		return nil
	})
	switch {
	case updateErr != nil && err == nil:
		// Return the updateSyncStatus error and re-reconcile
		return updateErr
	case err != nil:
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Return the upsertManagedObjects error and re-reconcile
		return err
	default: // both nil
		if updated {
			r.logger(ctx).Info("Setup successful")
		}
		return nil
	}
}

// teardown performs the following steps:
// - Delete managed objects
// - Convert any error into RootSync status conditions
// - Update the RootSync status
func (r *RootSyncReconciler) teardown(ctx context.Context, reconcilerRef types.NamespacedName, rs *v1beta1.RootSync) error {
	rsRef := client.ObjectKeyFromObject(rs)
	err := r.deleteManagedObjects(ctx, reconcilerRef, rsRef)
	updated, updateErr := r.updateSyncStatus(ctx, rs, reconcilerRef, func(syncObj *v1beta1.RootSync) error {
		// Modify the sync status,
		// but keep the upsert error separate from the status update error.
		err = r.handleReconcileError(ctx, err, syncObj, "Teardown")
		return nil
	})
	switch {
	case updateErr != nil && err == nil:
		// Return the updateSyncStatus error and re-reconcile
		return updateErr
	case err != nil:
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Return the upsertManagedObjects error and re-reconcile
		return err
	default: // both nil
		if updated {
			r.logger(ctx).Info("Teardown successful")
		}
		return nil
	}
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
			return NewNoRetryError(err) // no immediate retry - wait for next event
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

	if err != nil {
		err = errors.Wrap(err, stage)
	}
	return err // retry
}

// deleteManagedObjects deletes objects managed by the reconciler-manager for
// this RootSync.
func (r *RootSyncReconciler) deleteManagedObjects(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
	r.logger(ctx).Info("Deleting managed objects")

	if err := r.deleteDeployment(ctx, reconcilerRef); err != nil {
		return errors.Wrap(err, "deleting reconciler deployment")
	}

	// Note: ConfigMaps have been replaced by Deployment env vars.
	// Using env vars auto-updates the Deployment when they change.
	// This deletion remains to clean up after users upgrade.

	if err := r.deleteConfigMaps(ctx, reconcilerRef); err != nil {
		return errors.Wrap(err, "deleting config maps")
	}

	// Note: ReconcilerManager doesn't manage the RootSync Secret.
	// So we don't need to delete it here.

	if err := r.cleanRBACBindings(ctx, reconcilerRef, rsRef); err != nil {
		return errors.Wrap(err, "deleting RBAC bindings")
	}

	if err := r.deleteServiceAccount(ctx, reconcilerRef); err != nil {
		return errors.Wrap(err, "deleting service account")
	}

	return nil
}

// Register RootSync controller with reconciler-manager.
func (r *RootSyncReconciler) Register(mgr controllerruntime.Manager, watchFleetMembership bool) error {
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
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		// TODO: is it possible to watch with a label filter?
		Watches(&source.Kind{Type: &rbacv1.RoleBinding{}},
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
		//TODO: pass through context (reqs updating controller-runtime)
		ctx := context.Background()
		// Clear the membership if the cluster is unregistered
		if err := r.client.Get(ctx, types.NamespacedName{Name: fleetMembershipName}, &hubv1.Membership{}); err != nil {
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
	//TODO: pass through context (reqs updating controller-runtime)
	ctx := context.Background()
	objRef := client.ObjectKeyFromObject(obj)

	// Ignore changes from other namespaces.
	if objRef.Namespace != configsync.ControllerNamespace {
		return nil
	}

	// Look up RootSyncs and see if any of them reference this ConfigMap.
	rootSyncList := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, rootSyncList, client.InNamespace(objRef.Namespace)); err != nil {
		klog.Errorf("failed to list RootSyncs for %s: %v", kinds.ObjectSummary(obj), err)
		return nil
	}
	var requests []reconcile.Request
	var attachedRSNames []string
	for _, rs := range rootSyncList.Items {
		// Only enqueue a request for the RSync if it references the ConfigMap that triggered the event
		//TODO: Use stdlib slices.Contains in Go 1.21+
		if slices.Contains(rootSyncHelmValuesFileNames(&rs), objRef.Name) {
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&rs),
			})
			attachedRSNames = append(attachedRSNames, rs.GetName())
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to %s triggered a reconciliation for the RootSync(s) (%s)",
			kinds.ObjectSummary(obj), strings.Join(attachedRSNames, ", "))
	}
	return requests
}

func rootSyncHelmValuesFileNames(rs *v1beta1.RootSync) []string {
	if rs == nil {
		return nil
	}
	if rs.Spec.Helm == nil {
		return nil
	}
	if rs.Spec.Helm.ValuesFileRefs == nil {
		return nil
	}
	names := make([]string, len(rs.Spec.Helm.ValuesFileRefs))
	for i, ref := range rs.Spec.Helm.ValuesFileRefs {
		names[i] = ref.Name
	}
	return names
}

// mapObjectToRootSync define a mapping from an object in 'config-management-system'
// namespace to a RootSync to be reconciled.
func (r *RootSyncReconciler) mapObjectToRootSync(obj client.Object) []reconcile.Request {
	//TODO: pass through context (reqs updating controller-runtime)
	ctx := context.Background()
	objRef := client.ObjectKeyFromObject(obj)

	// Ignore changes from other namespaces because all the generated resources
	// exist in the config-management-system namespace.
	// Allow the empty namespace for ClusterRoleBindings.
	if objRef.Namespace != "" && objRef.Namespace != configsync.ControllerNamespace {
		return nil
	}

	// Ignore changes from resources without the root-reconciler prefix or configsync.gke.io:root-reconciler
	// because all the generated resources have the prefix.
	if !strings.HasPrefix(objRef.Name, core.RootReconcilerPrefix) &&
		!strings.HasPrefix(objRef.Name, RootSyncBaseClusterRoleName) {
		return nil
	}

	if err := r.addTypeInformationToObject(obj); err != nil {
		klog.Errorf("failed to lookup resource of object %T (%s): %v",
			obj, objRef, err)
		return nil
	}

	allRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, allRootSyncs); err != nil {
		klog.Errorf("failed to list all RootSyncs for %s (%s): %v",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, err)
		return nil
	}

	// Most of the resources are mapped to a single RootSync object except ClusterRoleBinding.
	// All RootSync objects share the same ClusterRoleBinding object, so requeue all RootSync objects if the ClusterRoleBinding is changed.
	// Note that there are two shared ClusterRoleBindings:
	// - configsync.gke.io/root-reconciler -> default binding to cluster-admin (for backwards compatibility)
	// - configsync.gke.io/root-reconciler-base -> created when roleRefs are in use (basic permissions, e.g. RootSync status update)
	// The additional RoleBindings and ClusterRoleBindings created from roleRefs
	// map to a single RootSync and are prefixed with the reconciler name.
	// For other resources, requeue the mapping RootSync object and then return.
	var requests []reconcile.Request
	var attachedRSNames []string
	for _, rs := range allRootSyncs.Items {
		reconcilerName := core.RootReconcilerName(rs.GetName())
		switch obj.(type) {
		case *rbacv1.ClusterRoleBinding:
			if objRef.Name == RootSyncLegacyClusterRoleBindingName || objRef.Name == RootSyncBaseClusterRoleBindingName {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&rs)})
				attachedRSNames = append(attachedRSNames, rs.GetName())
			} else if strings.HasPrefix(objRef.Name, reconcilerName) {
				return requeueRootSyncRequest(obj, &rs)
			}
		case *rbacv1.RoleBinding: // RoleBinding can be in any namespace
			if strings.HasPrefix(objRef.Name, reconcilerName) {
				return requeueRootSyncRequest(obj, &rs)
			}
		default: // Deployment and ServiceAccount are in the controller Namespace
			if objRef.Name == reconcilerName && objRef.Namespace == configsync.ControllerNamespace {
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
	//TODO: pass through context (reqs updating controller-runtime)
	ctx := context.Background()
	allRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, allRootSyncs); err != nil {
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
// RootSync objects via the following fields:
// - `spec.git.secretRef.name`
// - `spec.git.caCertSecretRef.name`
// - `spec.helm.secretRef.name`
// The update to the Secret object will trigger a reconciliation of the RootSync objects.
func (r *RootSyncReconciler) mapSecretToRootSyncs(secret client.Object) []reconcile.Request {
	//TODO: pass through context (reqs updating controller-runtime)
	ctx := context.Background()
	sRef := client.ObjectKeyFromObject(secret)
	// Ignore secret in other namespaces because the RootSync's secrets MUST
	// exist in the config-management-system namespace.
	if sRef.Namespace != configsync.ControllerNamespace {
		return nil
	}

	// Ignore secret starts with ns-reconciler prefix because those are for RepoSync objects.
	if strings.HasPrefix(sRef.Name, core.NsReconcilerPrefix+"-") {
		return nil
	}

	attachedRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, attachedRootSyncs, client.InNamespace(sRef.Namespace)); err != nil {
		klog.Errorf("RootSync list failed for Secret (%s): %v", sRef, err)
		return nil
	}

	var requests []reconcile.Request
	var attachedRSNames []string
	for _, rs := range attachedRootSyncs.Items {
		// Only enqueue a request for the RSync if it references the Secret that triggered the event
		switch sRef.Name {
		case rootSyncGitSecretName(&rs), rootSyncGitCACertSecretName(&rs),
			rootSyncOCICACertSecretName(&rs), rootSyncHelmCACertSecretName(&rs),
			rootSyncHelmSecretName(&rs):
			attachedRSNames = append(attachedRSNames, rs.GetName())
			requests = append(requests, reconcile.Request{
				NamespacedName: client.ObjectKeyFromObject(&rs),
			})
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to Secret (%s) triggers a reconciliation for the RootSync objects: %s",
			sRef, strings.Join(attachedRSNames, ", "))
	}
	return requests
}

func rootSyncGitSecretName(rs *v1beta1.RootSync) string {
	if rs == nil {
		return ""
	}
	if rs.Spec.Git == nil {
		return ""
	}
	if rs.Spec.Git.SecretRef == nil {
		return ""
	}
	return rs.Spec.Git.SecretRef.Name
}

func rootSyncGitCACertSecretName(rs *v1beta1.RootSync) string {
	if rs == nil {
		return ""
	}
	if rs.Spec.Git == nil {
		return ""
	}
	if rs.Spec.Git.CACertSecretRef == nil {
		return ""
	}
	return rs.Spec.Git.CACertSecretRef.Name
}

func rootSyncOCICACertSecretName(rs *v1beta1.RootSync) string {
	if rs == nil {
		return ""
	}
	if rs.Spec.Oci == nil {
		return ""
	}
	if rs.Spec.Oci.CACertSecretRef == nil {
		return ""
	}
	return rs.Spec.Oci.CACertSecretRef.Name
}

func rootSyncHelmCACertSecretName(rs *v1beta1.RootSync) string {
	if rs == nil {
		return ""
	}
	if rs.Spec.Helm == nil {
		return ""
	}
	if rs.Spec.Helm.CACertSecretRef == nil {
		return ""
	}
	return rs.Spec.Helm.CACertSecretRef.Name
}

func rootSyncHelmSecretName(rs *v1beta1.RootSync) string {
	if rs == nil {
		return ""
	}
	if rs.Spec.Helm == nil {
		return ""
	}
	if rs.Spec.Helm.SecretRef == nil {
		return ""
	}
	return rs.Spec.Helm.SecretRef.Name
}

func (r *RootSyncReconciler) populateContainerEnvs(ctx context.Context, rs *v1beta1.RootSync, reconcilerName string) map[string][]corev1.EnvVar {
	result := map[string][]corev1.EnvVar{
		reconcilermanager.HydrationController: hydrationEnvs(hydrationOptions{
			sourceType:     rs.Spec.SourceType,
			gitConfig:      rs.Spec.Git,
			ociConfig:      rs.Spec.Oci,
			scope:          declared.RootReconciler,
			reconcilerName: reconcilerName,
			pollPeriod:     r.hydrationPollingPeriod.String(),
		}),
		reconcilermanager.Reconciler: append(
			reconcilerEnvs(reconcilerOptions{
				clusterName:              r.clusterName,
				syncName:                 rs.Name,
				syncGeneration:           rs.Generation,
				reconcilerName:           reconcilerName,
				reconcilerScope:          declared.RootReconciler,
				sourceType:               rs.Spec.SourceType,
				gitConfig:                rs.Spec.Git,
				ociConfig:                rs.Spec.Oci,
				helmConfig:               rootsync.GetHelmBase(rs.Spec.Helm),
				pollPeriod:               r.reconcilerPollingPeriod.String(),
				statusMode:               rs.Spec.SafeOverride().StatusMode,
				reconcileTimeout:         v1beta1.GetReconcileTimeout(rs.Spec.SafeOverride().ReconcileTimeout),
				apiServerTimeout:         v1beta1.GetAPIServerTimeout(rs.Spec.SafeOverride().APIServerTimeout),
				requiresRendering:        annotationEnabled(metadata.RequiresRenderingAnnotationKey, rs.GetAnnotations()),
				dynamicNSSelectorEnabled: annotationEnabled(metadata.DynamicNSSelectorEnabledAnnotationKey, rs.GetAnnotations()),
			}),
			sourceFormatEnv(rs.Spec.SourceFormat),
			namespaceStrategyEnv(rs.Spec.SafeOverride().NamespaceStrategy),
		),
	}
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		result[reconcilermanager.GitSync] = gitSyncEnvs(ctx, options{
			ref:             rs.Spec.Git.Revision,
			branch:          rs.Spec.Git.Branch,
			repo:            rs.Spec.Git.Repo,
			secretType:      rs.Spec.Git.Auth,
			period:          v1beta1.GetPeriod(rs.Spec.Git.Period, configsync.DefaultReconcilerPollingPeriod),
			proxy:           rs.Spec.Proxy,
			depth:           rs.Spec.SafeOverride().GitSyncDepth,
			noSSLVerify:     rs.Spec.Git.NoSSLVerify,
			caCertSecretRef: v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef),
			knownHost:       r.isKnownHostsEnabled(rs.Spec.Git.Auth),
		})
		if enableAskpassSidecar(rs.Spec.SourceType, rs.Spec.Git.Auth) {
			result[reconcilermanager.GCENodeAskpassSidecar] = gceNodeAskPassSidecarEnvs(rs.Spec.GCPServiceAccountEmail)
		}
	case v1beta1.OciSource:
		result[reconcilermanager.OciSync] = ociSyncEnvs(ociOptions{
			image:           rs.Spec.Oci.Image,
			auth:            rs.Spec.Oci.Auth,
			period:          v1beta1.GetPeriod(rs.Spec.Oci.Period, configsync.DefaultReconcilerPollingPeriod).Seconds(),
			caCertSecretRef: v1beta1.GetSecretName(rs.Spec.Oci.CACertSecretRef),
		})
	case v1beta1.HelmSource:
		result[reconcilermanager.HelmSync] = helmSyncEnvs(helmOptions{
			helmBase:         &rs.Spec.Helm.HelmBase,
			releaseNamespace: rs.Spec.Helm.Namespace,
			deployNamespace:  rs.Spec.Helm.DeployNamespace,
			caCertSecretRef:  v1beta1.GetSecretName(rs.Spec.Helm.CACertSecretRef),
		})
	}
	return result
}

func (r *RootSyncReconciler) validateRootSync(ctx context.Context, rs *v1beta1.RootSync, reconcilerName string) error {
	if rs.Namespace != configsync.ControllerNamespace {
		return fmt.Errorf("RootSync objects are only allowed in the %s namespace, not in %s", configsync.ControllerNamespace, rs.Namespace)
	}

	if err := validation.IsDNS1123Subdomain(reconcilerName); err != nil {
		return fmt.Errorf("invalid reconciler name %q: %s", reconcilerName, strings.Join(err, ", "))
	}

	if err := r.validateSourceSpec(ctx, rs, reconcilerName); err != nil {
		return err
	}

	if err := r.validateRoleRefs(rs.Spec.SafeOverride().RoleRefs); err != nil {
		return err
	}

	return r.validateValuesFileSourcesRefs(ctx, rs)
}

func (r *RootSyncReconciler) validateSourceSpec(ctx context.Context, rs *v1beta1.RootSync, reconcilerName string) error {
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		return r.validateGitSpec(ctx, rs, reconcilerName)
	case v1beta1.OciSource:
		return r.validateOciSpec(ctx, rs)
	case v1beta1.HelmSource:
		return r.validateHelmSpec(ctx, rs)
	default:
		return validate.InvalidSourceType(rs)
	}
}

func (r *RootSyncReconciler) validateRoleRefs(roleRefs []v1beta1.RootSyncRoleRef) error {
	for _, roleRef := range roleRefs {
		if roleRef.Kind == "Role" && roleRef.Namespace == "" {
			return errors.Errorf("namespace must be provided for roleRef with kind Role.")
		}
	}
	return nil
}

// validateValuesFileSourcesRefs validates that the ConfigMaps specified in the RSync ValuesFileSources exist, are immutable, and have the
// specified data key.
func (r *RootSyncReconciler) validateValuesFileSourcesRefs(ctx context.Context, rs *v1beta1.RootSync) status.Error {
	if rs.Spec.SourceType != string(v1beta1.HelmSource) || rs.Spec.Helm == nil || len(rs.Spec.Helm.ValuesFileRefs) == 0 {
		return nil
	}
	return validate.ValuesFileRefs(ctx, r.client, rs, rs.Spec.Helm.ValuesFileRefs)
}

func (r *RootSyncReconciler) validateOciSpec(ctx context.Context, rs *v1beta1.RootSync) error {
	if err := validate.OciSpec(rs.Spec.Oci, rs); err != nil {
		return err
	}
	return r.validateCACertSecret(ctx, rs.Namespace, v1beta1.GetSecretName(rs.Spec.Oci.CACertSecretRef))
}

func (r *RootSyncReconciler) validateHelmSpec(ctx context.Context, rs *v1beta1.RootSync) error {
	if err := validate.HelmSpec(rootsync.GetHelmBase(rs.Spec.Helm), rs); err != nil {
		return err
	}
	if rs.Spec.Helm.Namespace != "" && rs.Spec.Helm.DeployNamespace != "" {
		if err := validate.HelmNSAndDeployNS(rs); err != nil {
			return err
		}
	}
	return r.validateCACertSecret(ctx, rs.Namespace, v1beta1.GetSecretName(rs.Spec.Helm.CACertSecretRef))
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

	_, r.knownHostExist = secret.Data[KnownHostsKey]

	return validateSecretData(rootSync.Spec.Auth, secret)
}

// listCurrentRoleRefs lists RBAC bindings in the cluster that were created by
// reconciler-manager based on previous roleRefs, queried using a label selector.
// A map is returned which maps RoleRef to RBAC binding for convenience to the caller.
func (r *RootSyncReconciler) listCurrentRoleRefs(ctx context.Context, rsRef types.NamespacedName) (map[v1beta1.RootSyncRoleRef]client.Object, error) {
	currentRoleMap := make(map[v1beta1.RootSyncRoleRef]client.Object)
	labelMap := ManagedObjectLabelMap(r.syncKind, rsRef)
	opts := &client.ListOptions{}
	opts.LabelSelector = client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(labelMap),
	}
	rbList := rbacv1.RoleBindingList{}
	if err := r.client.List(ctx, &rbList, opts); err != nil {
		return nil, errors.Wrap(err, "listing RoleBindings")
	}
	for idx, rb := range rbList.Items {
		roleRef := v1beta1.RootSyncRoleRef{
			Kind:      rb.RoleRef.Kind,
			Name:      rb.RoleRef.Name,
			Namespace: rb.Namespace,
		}
		currentRoleMap[roleRef] = &rbList.Items[idx]
	}
	crbList := rbacv1.ClusterRoleBindingList{}
	if err := r.client.List(ctx, &crbList, opts); err != nil {
		return nil, errors.Wrap(err, "listing ClusterRoleBindings")
	}
	for idx, crb := range crbList.Items {
		roleRef := v1beta1.RootSyncRoleRef{
			Kind: crb.RoleRef.Kind,
			Name: crb.RoleRef.Name,
		}
		currentRoleMap[roleRef] = &crbList.Items[idx]
	}
	return currentRoleMap, nil
}

func (r *RootSyncReconciler) cleanRBACBindings(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
	currentRefMap, err := r.listCurrentRoleRefs(ctx, rsRef)
	if err != nil {
		return err
	}
	for _, binding := range currentRefMap {
		if err := r.cleanup(ctx, binding); err != nil {
			return errors.Wrap(err, "deleting RBAC Binding")
		}
	}
	if err := r.deleteSharedClusterRoleBinding(ctx, RootSyncLegacyClusterRoleBindingName, reconcilerRef); err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "deleting legacy binding")
	}
	if err := r.deleteSharedClusterRoleBinding(ctx, RootSyncBaseClusterRoleBindingName, reconcilerRef); err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "deleting base binding")
	}
	return nil
}

// manageRBACBindings will reconcile the managed RBAC bindings on the cluster
// with what is declared in spec.overrides.roleRefs.
func (r *RootSyncReconciler) manageRBACBindings(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, roleRefs []v1beta1.RootSyncRoleRef) error {
	currentRefMap, err := r.listCurrentRoleRefs(ctx, rsRef)
	if err != nil {
		return err
	}
	if len(roleRefs) == 0 {
		// Backwards compatible behavior to default to cluster-admin
		if err := r.upsertSharedClusterRoleBinding(ctx, RootSyncLegacyClusterRoleBindingName, "cluster-admin", reconcilerRef, rsRef); err != nil {
			return err
		}
		// Clean up any RoleRefs created previously that are no longer declared
		for _, binding := range currentRefMap {
			if err := r.cleanup(ctx, binding); err != nil {
				return errors.Wrap(err, "deleting RBAC Binding")
			}
		}
		if err := r.deleteSharedClusterRoleBinding(ctx, RootSyncBaseClusterRoleBindingName, reconcilerRef); err != nil && !apierrors.IsNotFound(err) {
			return errors.Wrap(err, "deleting base binding")
		}
		return nil
	}
	// Add the base ClusterRole for basic root reconciler functionality
	if err := r.upsertSharedClusterRoleBinding(ctx, RootSyncBaseClusterRoleBindingName, RootSyncBaseClusterRoleName, reconcilerRef, rsRef); err != nil {
		return err
	}
	declaredRoleRefs := roleRefs
	// Create RoleBindings which are declared but do not exist on the cluster
	for _, roleRef := range declaredRoleRefs {
		if _, ok := currentRefMap[roleRef]; !ok {
			// we need to call a separate create method here for generateName
			if _, err := r.createRBACBinding(ctx, reconcilerRef, rsRef, roleRef); err != nil {
				return errors.Wrap(err, "creating RBAC Binding")
			}
		}
	}
	// convert to map for lookup
	declaredRefMap := make(map[v1beta1.RootSyncRoleRef]bool)
	for _, roleRef := range declaredRoleRefs {
		declaredRefMap[roleRef] = true
	}
	// For existing RoleBindings:
	// - if they are declared in roleRefs, update
	// - if they are no longer declared in roleRefs, delete
	for roleRef, binding := range currentRefMap {
		if _, ok := declaredRefMap[roleRef]; ok { // update
			if err := r.updateRBACBinding(ctx, reconcilerRef, rsRef, binding); err != nil {
				return errors.Wrap(err, "upserting RBAC Binding")
			}
		} else { // Clean up any RoleRefs created previously that are no longer declared
			if err := r.cleanup(ctx, binding); err != nil {
				return errors.Wrap(err, "deleting RBAC Binding")
			}
		}
	}
	// in order to be backwards compatible with behavior before https://github.com/GoogleContainerTools/kpt-config-sync/pull/938
	// we delete any ClusterRoleBinding that uses the old name (configsync.gke.io:root-reconciler).
	// In older versions, this ClusterRoleBinding was always bound to cluster-admin.
	// This ensures smooth migrations for users upgrading past that version boundary.
	if err := r.deleteSharedClusterRoleBinding(ctx, RootSyncLegacyClusterRoleBindingName, reconcilerRef); err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "deleting legacy binding")
	}
	return nil
}

func (r *RootSyncReconciler) upsertSharedClusterRoleBinding(ctx context.Context, name, clusterRole string, reconcilerRef, rsRef types.NamespacedName) error {
	crbRef := client.ObjectKey{Name: name}
	childCRB := &rbacv1.ClusterRoleBinding{}
	childCRB.Name = crbRef.Name

	labelMap := ManagedObjectLabelMap(r.syncKind, rsRef)
	// Remove sync-name label since the ClusterRoleBinding may be shared
	delete(labelMap, metadata.SyncNameLabel)

	op, err := CreateOrUpdate(ctx, r.client, childCRB, func() error {
		core.AddLabels(childCRB, labelMap)
		childCRB.OwnerReferences = nil
		childCRB.RoleRef = rolereference(clusterRole, "ClusterRole")
		childCRB.Subjects = addSubject(childCRB.Subjects, r.serviceAccountSubject(reconcilerRef))
		// Remove existing OwnerReferences, now that we're using finalizers.
		childCRB.OwnerReferences = nil
		return nil
	})
	if err != nil {
		return err
	}
	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, crbRef.String(),
			logFieldObjectKind, "ClusterRoleBinding",
			logFieldOperation, op)
	}
	return nil
}

func (r *RootSyncReconciler) createRBACBinding(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, roleRef v1beta1.RootSyncRoleRef) (client.ObjectKey, error) {
	var binding client.Object
	if roleRef.Namespace == "" {
		crb := rbacv1.ClusterRoleBinding{}
		crb.RoleRef = rolereference(roleRef.Name, roleRef.Kind)
		crb.Subjects = []rbacv1.Subject{r.serviceAccountSubject(reconcilerRef)}
		binding = &crb
	} else {
		rb := rbacv1.RoleBinding{}
		rb.Namespace = roleRef.Namespace
		rb.RoleRef = rolereference(roleRef.Name, roleRef.Kind)
		rb.Subjects = []rbacv1.Subject{r.serviceAccountSubject(reconcilerRef)}
		binding = &rb
	}
	// use generateName to produce a unique name. A predictable unique name
	// is not feasible, since it would risk hitting length constraints.
	// e.g. reconciler.name + roleRef.kind + roleRef.name
	binding.SetGenerateName(fmt.Sprintf("%s-", reconcilerRef.Name))
	binding.SetLabels(ManagedObjectLabelMap(r.syncKind, rsRef))

	if err := r.client.Create(ctx, binding); err != nil {
		return client.ObjectKey{}, err
	}
	rbRef := client.ObjectKey{
		Name:      binding.GetName(),
		Namespace: binding.GetNamespace(),
	}
	r.logger(ctx).Info("Managed object create successful",
		logFieldObjectRef, rbRef.String(),
		logFieldObjectKind, binding.GetObjectKind().GroupVersionKind().Kind)
	return rbRef, nil
}

func (r *RootSyncReconciler) updateSyncStatus(ctx context.Context, rs *v1beta1.RootSync, reconcilerRef types.NamespacedName, updateFn func(*v1beta1.RootSync) error) (bool, error) {
	// Always set the reconciler and observedGeneration when updating sync status
	updateFn2 := func(syncObj *v1beta1.RootSync) error {
		err := updateFn(syncObj)
		syncObj.Status.Reconciler = reconcilerRef.Name
		syncObj.Status.ObservedGeneration = syncObj.Generation
		return err
	}

	updated, err := mutate.Status(ctx, r.client, rs, func() error {
		before := rs.DeepCopy()
		if err := updateFn2(rs); err != nil {
			return err
		}
		// TODO: Fix the status condition helpers to not update the timestamps if nothing changed.
		// There's no good way to do a semantic comparison that ignores timestamps.
		// So we're doing both for now to try to prevent updates whenever possible.
		if equality.Semantic.DeepEqual(before.Status, rs.Status) {
			// No update necessary.
			return &mutate.NoUpdateError{}
		}
		if cmp.Equal(before.Status, rs.Status, compare.IgnoreTimestampUpdates) {
			// No update necessary.
			return &mutate.NoUpdateError{}
		}
		if r.logger(ctx).V(5).Enabled() {
			r.logger(ctx).Info("Updating sync status",
				logFieldResourceVersion, rs.ResourceVersion,
				"diff", fmt.Sprintf("Diff (- Expected, + Actual):\n%s",
					cmp.Diff(before.Status, rs.Status)))
		}
		return nil
	})
	if err != nil {
		return updated, errors.Wrapf(err, "Sync status update failed")
	}
	if updated {
		r.logger(ctx).Info("Sync status update successful")
	} else {
		r.logger(ctx).V(5).Info("Sync status update skipped: no change")
	}
	return updated, nil
}

// annotationEnabled returns whether the annotation should be enabled for the
// reconciler of this RSync. This is determined by an annotation that is set on
// the RSync by the reconciler.
func annotationEnabled(key string, annotations map[string]string) bool {
	val, ok := annotations[key]
	if !ok { // default to disabling the annotation
		return false
	}
	boolVal, err := strconv.ParseBool(val)
	if err != nil {
		// This should never happen, as the annotation should always be set to a
		// valid value by the reconciler. Log the error and return the default value.
		klog.Infof("failed to parse %s value to boolean: %s", key, val)
		return false
	}
	return boolVal
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
			caCertSecretRefName = v1beta1.GetSecretName(rs.Spec.Oci.CACertSecretRef)
		case v1beta1.HelmSource:
			auth = rs.Spec.Helm.Auth
			gcpSAEmail = rs.Spec.Helm.GCPServiceAccountEmail
			secretRefName = v1beta1.GetSecretName(rs.Spec.Helm.SecretRef)
			caCertSecretRefName = v1beta1.GetSecretName(rs.Spec.Helm.CACertSecretRef)
		}
		injectFWICreds := useFWIAuth(auth, r.membership)
		if injectFWICreds {
			if err := r.injectFleetWorkloadIdentityCredentials(&d.Spec.Template, gcpSAEmail); err != nil {
				return err
			}
		}

		// Add sync-generation label
		core.SetLabel(&d.ObjectMeta, metadata.SyncGenerationLabel, fmt.Sprint(rs.GetGeneration()))
		core.SetLabel(&d.Spec.Template, metadata.SyncGenerationLabel, fmt.Sprint(rs.GetGeneration()))

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

		autopilot, err := r.isAutopilot()
		if err != nil {
			return err
		}
		var containerResourceDefaults map[string]v1beta1.ContainerResourcesSpec
		if autopilot {
			containerResourceDefaults = ReconcilerContainerResourceDefaultsForAutopilot()
		} else {
			containerResourceDefaults = ReconcilerContainerResourceDefaults()
		}
		var containerLogLevelDefaults = ReconcilerContainerLogLevelDefaults()

		overrides := rs.Spec.SafeOverride()
		containerResources := setContainerResourceDefaults(overrides.Resources,
			containerResourceDefaults)
		containerLogLevels := setContainerLogLevelDefaults(overrides.LogLevels, containerLogLevelDefaults)

		var updatedContainers []corev1.Container
		for _, container := range templateSpec.Containers {
			addContainer := true
			switch container.Name {
			case reconcilermanager.Reconciler:
				container.Env = append(container.Env, containerEnvs[container.Name]...)
			case reconcilermanager.HydrationController:
				if !annotationEnabled(metadata.RequiresRenderingAnnotationKey, rs.GetAnnotations()) {
					// if the sync source does not require rendering, omit the hydration controller
					// this minimizes the resource footprint of the reconciler
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					container.Image = updateHydrationControllerImage(container.Image, rs.Spec.SafeOverride().OverrideSpec)
				}
			case reconcilermanager.OciSync:
				// Don't add the oci-sync container when sourceType is NOT oci.
				if v1beta1.SourceType(rs.Spec.SourceType) != v1beta1.OciSource {
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					container.VolumeMounts = volumeMounts(rs.Spec.Oci.Auth, caCertSecretRefName, rs.Spec.SourceType, container.VolumeMounts)
					injectFWICredsToContainer(&container, injectFWICreds)
				}
			case reconcilermanager.HelmSync:
				// Don't add the helm-sync container when sourceType is NOT helm.
				if v1beta1.SourceType(rs.Spec.SourceType) != v1beta1.HelmSource {
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					container.VolumeMounts = volumeMounts(rs.Spec.Helm.Auth, caCertSecretRefName, rs.Spec.SourceType, container.VolumeMounts)
					if authTypeToken(rs.Spec.Helm.Auth) {
						container.Env = append(container.Env, helmSyncTokenAuthEnv(secretRefName)...)
					}
					mountConfigMapValuesFiles(templateSpec, &container, r.getReconcilerHelmConfigMapRefs(rs))
					injectFWICredsToContainer(&container, injectFWICreds)
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
				}
			case reconcilermanager.GCENodeAskpassSidecar:
				if !enableAskpassSidecar(rs.Spec.SourceType, auth) {
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					injectFWICredsToContainer(&container, injectFWICreds)
					// TODO: enable resource/logLevel overrides for gcenode-askpass-sidecar
				}
			case metrics.OtelAgentName:
				container.Env = append(container.Env, containerEnvs[container.Name]...)
			default:
				return errors.Errorf("unknown container in reconciler deployment template: %q", container.Name)
			}
			if addContainer {
				// Common mutations for all containers
				mutateContainerResource(&container, containerResources)
				if err := mutateContainerLogLevel(&container, containerLogLevels); err != nil {
					return err
				}
				updatedContainers = append(updatedContainers, container)
			}
		}

		templateSpec.Containers = updatedContainers
		return nil
	}
}
