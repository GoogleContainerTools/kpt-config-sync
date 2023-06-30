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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
	v1 "kpt.dev/configsync/pkg/api/configmanagement/v1"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/kinds"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/reposync"
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

// RepoSyncReconciler reconciles a RepoSync object.
type RepoSyncReconciler struct {
	reconcilerBase

	// lock ensures that the Reconcile method only runs one at a time.
	lock sync.Mutex

	// configMapWatches stores which namespaces where we are currently watching ConfigMaps
	configMapWatches map[string]bool

	controller *controller.Controller
}

// NewRepoSyncReconciler returns a new RepoSyncReconciler.
func NewRepoSyncReconciler(clusterName string, reconcilerPollingPeriod, hydrationPollingPeriod, helmSyncVersionPollingPeriod time.Duration, client client.Client, watcher client.WithWatch, dynamicClient dynamic.Interface, log logr.Logger, scheme *runtime.Scheme) *RepoSyncReconciler {
	return &RepoSyncReconciler{
		reconcilerBase: reconcilerBase{
			loggingController: loggingController{
				log: log,
			},
			clusterName:                  clusterName,
			client:                       client,
			dynamicClient:                dynamicClient,
			watcher:                      watcher,
			scheme:                       scheme,
			reconcilerPollingPeriod:      reconcilerPollingPeriod,
			hydrationPollingPeriod:       hydrationPollingPeriod,
			helmSyncVersionPollingPeriod: helmSyncVersionPollingPeriod,
			syncKind:                     configsync.RepoSyncKind,
		},
		configMapWatches: make(map[string]bool),
	}
}

// +kubebuilder:rbac:groups=configsync.gke.io,resources=reposyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=configsync.gke.io,resources=reposyncs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile the RepoSync resource.
func (r *RepoSyncReconciler) Reconcile(ctx context.Context, req controllerruntime.Request) (controllerruntime.Result, error) {
	r.lock.Lock()
	defer r.lock.Unlock()

	rsRef := req.NamespacedName
	start := time.Now()
	reconcilerRef := types.NamespacedName{
		Namespace: v1.NSConfigManagementSystem,
		Name:      core.NsReconcilerName(rsRef.Namespace, rsRef.Name),
	}
	ctx = r.setLoggerValues(ctx,
		logFieldSyncKind, r.syncKind,
		logFieldSyncRef, rsRef.String(),
		logFieldReconciler, reconcilerRef.String())
	rs := &v1beta1.RepoSync{}

	if err := r.client.Get(ctx, rsRef, rs); err != nil {
		if apierrors.IsNotFound(err) {
			// Cleanup after already deleted RepoSync.
			// This code path is unlikely, because the custom finalizer should
			// have already deleted the managed resources and removed the
			// repoSyncs cache entry. But if we get here, clean up anyway.
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
		return controllerruntime.Result{}, status.APIServerError(err, "failed to get RepoSync")
	}

	if !rs.DeletionTimestamp.IsZero() {
		// Deletion requested.
		// Cleanup is handled above, after the RepoSync is NotFound.
		r.logger(ctx).V(3).Info("Sync deletion timestamp detected")
	}

	currentRS := rs.DeepCopy()

	if err := r.watchConfigMaps(rs); err != nil {
		r.logger(ctx).Error(err, "Error watching ConfigMaps")
		reposync.SetStalled(rs, "ConfigMapWatch", err)
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Error setting status")
		}
		// the error may be recoverable, so we retry (return the error)
		return controllerruntime.Result{}, err
	}

	if err := r.validateNamespaceName(rsRef.Namespace); err != nil {
		r.logger(ctx).Error(err, "Sync namespace invalid")
		reposync.SetStalled(rs, "Validation", err)
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
		reposync.SetStalled(rs, "Validation", err)
		// Validation errors should not trigger retry (return error),
		// unless the status update also fails.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		// Use the validation error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, updateErr
	}

	if err := r.validateSpec(ctx, rs, reconcilerRef.Name); err != nil {
		r.logger(ctx).Error(err, "Sync spec invalid")
		reposync.SetStalled(rs, "Validation", err)
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

func (r *RepoSyncReconciler) upsertManagedObjects(ctx context.Context, reconcilerRef types.NamespacedName, rs *v1beta1.RepoSync) error {
	rsRef := client.ObjectKeyFromObject(rs)
	r.logger(ctx).V(3).Info("Reconciling managed objects")

	// Create secret in config-management-system namespace using the
	// existing secret in the reposync.namespace.
	if _, err := r.upsertAuthSecret(ctx, rs, reconcilerRef); err != nil {
		return err
	}

	// Create secret in config-management-system namespace using the
	// existing secret in the reposync.namespace.
	if _, err := r.upsertCACertSecret(ctx, rs, reconcilerRef); err != nil {
		return err
	}

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

	// Overwrite reconciler rolebinding.
	if _, err := r.upsertRoleBinding(ctx, reconcilerRef, rsRef); err != nil {
		return err
	}

	containerEnvs := r.populateContainerEnvs(ctx, rs, reconcilerRef.Name)
	newValuesFrom, err := copyConfigMapsToCms(ctx, r.client, rs)
	if err != nil {
		return errors.Errorf("unable to copy ConfigMapRefs to config-management-system namespace: %s", err.Error())
	}
	mut := r.mutationsFor(ctx, rs, containerEnvs, newValuesFrom)

	// Upsert Namespace reconciler deployment.
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
// - Convert any error into RepoSync status conditions
// - Update the RepoSync status
func (r *RepoSyncReconciler) setup(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, currentRS, rs *v1beta1.RepoSync) error {
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

// teardown performs the following teardown steps:
// - Delete managed objects
// - Convert any error into RepoSync status conditions
// - Update the RepoSync status
func (r *RepoSyncReconciler) teardown(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, currentRS, rs *v1beta1.RepoSync) error {
	err := r.deleteManagedObjects(ctx, reconcilerRef, rsRef)
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
func (r *RepoSyncReconciler) handleReconcileError(ctx context.Context, err error, rs *v1beta1.RepoSync, stage string) error {
	if err == nil {
		reposync.ClearCondition(rs, v1beta1.RepoSyncReconciling)
		reposync.ClearCondition(rs, v1beta1.RepoSyncStalled)
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
		reposync.SetReconciling(rs, stage, fmt.Sprintf("%s stalled", stage))
		reposync.SetStalled(rs, opErr.ID.Kind, err)
	} else if errors.As(err, &statusErr) {
		// Metadata from ObjectReconcileError used for log context
		r.logger(ctx).Error(err, fmt.Sprintf("%s waiting for event", stage),
			logFieldObjectRef, statusErr.ID.ObjectKey.String(),
			logFieldObjectKind, statusErr.ID.Kind,
			logFieldObjectStatus, statusErr.Status)
		switch statusErr.Status {
		case kstatus.InProgressStatus, kstatus.TerminatingStatus:
			// still making progress
			reposync.SetReconciling(rs, statusErr.ID.Kind, err.Error())
			reposync.ClearCondition(rs, v1beta1.RepoSyncStalled)
			return nil // no immediate retry - wait for next event
		default:
			// failed or invalid
			reposync.SetReconciling(rs, stage, fmt.Sprintf("%s stalled", stage))
			reposync.SetStalled(rs, statusErr.ID.Kind, err)
		}
	} else {
		r.logger(ctx).Error(err, fmt.Sprintf("%s failed", stage))
		reposync.SetReconciling(rs, stage, fmt.Sprintf("%s stalled", stage))
		reposync.SetStalled(rs, "Error", err)
	}
	return err // retry
}

// deleteManagedObjects deletes objects managed by the reconciler-manager for
// this RepoSync. If updateStatus is true, the RepoSync status will be updated
// to reflect the teardown progress.
func (r *RepoSyncReconciler) deleteManagedObjects(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
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

	if err := r.deleteSecrets(ctx, reconcilerRef); err != nil {
		return err
	}

	if err := r.deleteRoleBinding(ctx, reconcilerRef, rsRef); err != nil {
		return err
	}

	return r.deleteServiceAccount(ctx, reconcilerRef)
}

// SetupWithManager registers RepoSync controller with reconciler-manager.
func (r *RepoSyncReconciler) SetupWithManager(mgr controllerruntime.Manager, watchFleetMembership bool) error {
	// Index the `gitSecretRefName` field, so that we will be able to lookup RepoSync be a referenced `SecretRef` name.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1beta1.RepoSync{}, gitSecretRefField, func(rawObj client.Object) []string {
		rs, ok := rawObj.(*v1beta1.RepoSync)
		if !ok {
			// Only add index for RepoSync
			return nil
		}
		if rs.Spec.Git == nil || v1beta1.GetSecretName(rs.Spec.Git.SecretRef) == "" {
			return nil
		}
		return []string{rs.Spec.Git.SecretRef.Name}
	}); err != nil {
		return err
	}
	// Index the `caCertSecretRefField` field, so that we will be able to lookup RepoSync be a referenced `caCertSecretRefField` name.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1beta1.RepoSync{}, caCertSecretRefField, func(rawObj client.Object) []string {
		rs, ok := rawObj.(*v1beta1.RepoSync)
		if !ok {
			// Only add index for RepoSync
			return nil
		}
		if rs.Spec.Git == nil || v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef) == "" {
			return nil
		}
		return []string{rs.Spec.Git.CACertSecretRef.Name}
	}); err != nil {
		return err
	}
	// Index the `helmSecretRefName` field, so that we will be able to lookup RepoSync be a referenced `SecretRef` name.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1beta1.RepoSync{}, helmSecretRefField, func(rawObj client.Object) []string {
		rs, ok := rawObj.(*v1beta1.RepoSync)
		if !ok {
			// Only add index for RepoSync
			return nil
		}
		if rs.Spec.Helm == nil || v1beta1.GetSecretName(rs.Spec.Helm.SecretRef) == "" {
			return nil
		}
		return []string{rs.Spec.Helm.SecretRef.Name}
	}); err != nil {
		return err
	}

	controllerBuilder := controllerruntime.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		For(&v1beta1.RepoSync{}).
		// Custom Watch to trigger Reconcile for objects created by RepoSync controller.
		Watches(&source.Kind{Type: &corev1.Secret{}},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToRepoSyncs),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&source.Kind{Type: withNamespace(&appsv1.Deployment{}, configsync.ControllerNamespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectToRepoSync),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&source.Kind{Type: withNamespace(&corev1.ServiceAccount{}, configsync.ControllerNamespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectToRepoSync),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		Watches(&source.Kind{Type: withNamespace(&rbacv1.RoleBinding{}, configsync.ControllerNamespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectToRepoSync),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}))

	if watchFleetMembership {
		// Custom Watch for membership to trigger reconciliation.
		controllerBuilder.Watches(&source.Kind{Type: &hubv1.Membership{}},
			handler.EnqueueRequestsFromMapFunc(r.mapMembershipToRepoSyncs()),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	}

	ctrlr, err := controllerBuilder.Build(r)
	r.controller = &ctrlr

	return err
}

func (r *RepoSyncReconciler) watchConfigMaps(rs *v1beta1.RepoSync) error {
	// We add watches dynamically at runtime based on the RepoSync namespace
	// in order to avoid watching ConfigMaps in the entire cluster.
	if rs == nil || rs.Spec.SourceType != string(v1beta1.HelmSource) || rs.Spec.Helm == nil ||
		len(rs.Spec.Helm.ValuesFileSources) == 0 {
		// TODO: When it's available, we should remove unneeded watches from the controller
		// when all RepoSyncs with ConfigMap references in a particular namespace are
		// deleted (or are no longer referencing ConfigMaps).
		// See https://github.com/kubernetes-sigs/controller-runtime/pull/2159
		// and https://github.com/kubernetes-sigs/controller-runtime/issues/1884
		return nil
	}

	if _, ok := r.configMapWatches[rs.Namespace]; !ok {
		klog.Infoln("Adding watch for ConfigMaps in namespace ", rs.Namespace)
		ctrlr := *r.controller

		if err := ctrlr.Watch(&source.Kind{Type: withNamespace(&corev1.ConfigMap{}, rs.Namespace)},
			handler.EnqueueRequestsFromMapFunc(r.mapConfigMapToRepoSyncs),
			predicate.ResourceVersionChangedPredicate{}); err != nil {
			return err
		}

		r.configMapWatches[rs.Namespace] = true
	}
	return nil
}

func (r *RepoSyncReconciler) mapMembershipToRepoSyncs() func(client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		// Clear the membership if the cluster is unregistered
		if err := r.client.Get(context.Background(), types.NamespacedName{Name: fleetMembershipName}, &hubv1.Membership{}); err != nil {
			if apierrors.IsNotFound(err) {
				klog.Info("Fleet Membership not found, clearing membership cache")
				r.membership = nil
				return r.requeueAllRepoSyncs()
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
		return r.requeueAllRepoSyncs()
	}
}

func (r *RepoSyncReconciler) requeueAllRepoSyncs() []reconcile.Request {
	allRepoSyncs := &v1beta1.RepoSyncList{}
	if err := r.client.List(context.Background(), allRepoSyncs); err != nil {
		klog.Errorf("RepoSync list failed: %v", err)
		return nil
	}

	requests := make([]reconcile.Request, len(allRepoSyncs.Items))
	for i, rs := range allRepoSyncs.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&rs),
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to membership trigger reconciliations for %d RepoSync objects.", len(allRepoSyncs.Items))
	}
	return requests
}

// mapSecretToRepoSyncs define a mapping from the Secret object to its attached
// RepoSync objects via the `spec.git.secretRef.name` field .
// The update to the Secret object will trigger a reconciliation of the RepoSync objects.
func (r *RepoSyncReconciler) mapSecretToRepoSyncs(secret client.Object) []reconcile.Request {
	sRef := client.ObjectKeyFromObject(secret)
	// map the copied ns-reconciler Secret in the config-management-system to RepoSync request.
	if sRef.Namespace == configsync.ControllerNamespace {
		// Ignore secrets in the config-management-system namespace that don't start with ns-reconciler.
		if !strings.HasPrefix(sRef.Name, core.NsReconcilerPrefix) {
			return nil
		}
		if err := r.addTypeInformationToObject(secret); err != nil {
			klog.Errorf("Failed to add type to object (%s): %v", sRef, err)
			return nil
		}
		allRepoSyncs := &v1beta1.RepoSyncList{}
		if err := r.client.List(context.Background(), allRepoSyncs); err != nil {
			klog.Errorf("RepoSync list failed for Secret (%s): %v", sRef, err)
			return nil
		}
		for _, rs := range allRepoSyncs.Items {
			// It is a one-to-one mapping between the copied ns-reconciler Secret and the RepoSync object,
			// so requeue the mapped RepoSync object and then return.
			reconcilerName := core.NsReconcilerName(rs.GetNamespace(), rs.GetName())
			if isUpsertedSecret(&rs, sRef.Name) {
				return requeueRepoSyncRequest(secret, &rs)
			}
			isSAToken := strings.HasPrefix(sRef.Name, reconcilerName+"-token-")
			if isSAToken {
				saRef := client.ObjectKey{
					Name:      reconcilerName,
					Namespace: configsync.ControllerNamespace,
				}
				serviceAccount := &corev1.ServiceAccount{}
				if err := r.client.Get(context.Background(), saRef, serviceAccount); err != nil {
					klog.Errorf("ServiceAccount get failed (%s): %v", saRef, err)
					return nil
				}
				for _, s := range serviceAccount.Secrets {
					if s.Name == sRef.Name {
						return requeueRepoSyncRequest(secret, &rs)
					}
				}
			}
		}
		return nil
	}

	// map the user-managed ns-reconciler Secret in the RepoSync's namespace to RepoSync request.
	// The user-managed ns-reconciler Secret might be shared among multiple RepoSync objects in the same namespace,
	// so requeue all the attached RepoSync objects.
	attachedRepoSyncs := &v1beta1.RepoSyncList{}
	secretFields := []string{gitSecretRefField, caCertSecretRefField, helmSecretRefField}
	for _, secretField := range secretFields {
		listOps := &client.ListOptions{
			FieldSelector: fields.OneTermEqualSelector(secretField, sRef.Name),
			Namespace:     sRef.Namespace,
		}
		fetchedRepoSyncs := &v1beta1.RepoSyncList{}
		if err := r.client.List(context.Background(), fetchedRepoSyncs, listOps); err != nil {
			klog.Errorf("RepoSync list failed for Secret (%s): %v", sRef, err)
			return nil
		}
		attachedRepoSyncs.Items = append(attachedRepoSyncs.Items, fetchedRepoSyncs.Items...)
	}
	requests := make([]reconcile.Request, len(attachedRepoSyncs.Items))
	attachedRSNames := make([]string, len(attachedRepoSyncs.Items))
	for i, rs := range attachedRepoSyncs.Items {
		attachedRSNames[i] = rs.GetName()
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&rs),
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to Secret (%s) triggers a reconciliation for the RepoSync object %q in the same namespace.",
			sRef, strings.Join(attachedRSNames, ", "))
	}
	return requests
}

func (r *RepoSyncReconciler) mapConfigMapToRepoSyncs(obj client.Object) []reconcile.Request {
	objRef := client.ObjectKeyFromObject(obj)
	if objRef.Namespace == v1.NSConfigManagementSystem {
		// ignore ConfigMaps in the config-management-system namespace
		return nil
	}

	ctx := context.Background()
	allRepoSyncs := &v1beta1.RepoSyncList{}
	if err := r.client.List(ctx, allRepoSyncs); err != nil {
		klog.Error("failed to list all RepoSyncs for %s (%s): %v",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, err)
		return nil
	}

	var result []reconcile.Request
	for _, rs := range allRepoSyncs.Items {
		if rs.Namespace != objRef.Namespace {
			// ignore objects that do not match the RepoSync namespace
			continue
		}
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
			// requeue the reconcile to surface the validation errors in the status
			result = append(result, requeueRepoSyncRequest(obj, &rs)...)
			continue
		}

		if _, err := copyOneConfigMapToCms(ctx, r.client, objRef); err != nil {
			klog.Errorf("error copying updated configmap to config-management-system namespace: %s", err.Error)
		}

		rsRef := client.ObjectKeyFromObject(&rs)
		reconcilerRef := types.NamespacedName{
			Namespace: v1.NSConfigManagementSystem,
			Name:      core.NsReconcilerName(rsRef.Namespace, rsRef.Name),
		}

		var reconciler appsv1.Deployment
		cl := r.reconcilerBase.client
		if err := cl.Get(ctx, reconcilerRef, &reconciler); err != nil {
			klog.Errorf("failed to get reconciler reference: %s", err.Error())
			if apierrors.IsNotFound(err) {
				// if the root-reconciler doesn't exist, an updated ConfigMap should trigger a reconcile
				// to create one
				result = append(result, requeueRepoSyncRequest(obj, &rs)...)
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
		result = append(result, requeueRepoSyncRequest(obj, &rs)...)

	}
	return result
}

// mapObjectToRepoSync define a mapping from an object in 'config-management-system'
// namespace to a RepoSync to be reconciled.
func (r *RepoSyncReconciler) mapObjectToRepoSync(obj client.Object) []reconcile.Request {
	objRef := client.ObjectKeyFromObject(obj)

	// Ignore changes from other namespaces because all the generated resources
	// exist in the config-management-system namespace.
	if objRef.Namespace != configsync.ControllerNamespace {
		return nil
	}

	// Ignore changes from resources without the ns-reconciler prefix or configsync.gke.io:ns-reconciler
	// because all the generated resources have the prefix.
	nsRoleBindingName := RepoSyncPermissionsName()
	if !strings.HasPrefix(objRef.Name, core.NsReconcilerPrefix) && objRef.Name != nsRoleBindingName {
		return nil
	}

	if err := r.addTypeInformationToObject(obj); err != nil {
		klog.Errorf("failed to lookup resource of object %T (%s): %v",
			obj, objRef, err)
		return nil
	}

	allRepoSyncs := &v1beta1.RepoSyncList{}
	if err := r.client.List(context.Background(), allRepoSyncs); err != nil {
		klog.Errorf("failed to list all RepoSyncs for %s (%s): %v",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, err)
		return nil
	}

	// Most of the resources are mapped to a single RepoSync object except RoleBinding.
	// All RepoSync objects share the same RoleBinding object, so requeue all RepoSync objects if the RoleBinding is changed.
	// For other resources, requeue the mapping RepoSync object and then return.
	var requests []reconcile.Request
	var attachedRSNames []string
	for _, rs := range allRepoSyncs.Items {
		reconcilerName := core.NsReconcilerName(rs.GetNamespace(), rs.GetName())
		switch obj.(type) {
		case *rbacv1.RoleBinding:
			if objRef.Name == nsRoleBindingName {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&rs),
				})
				attachedRSNames = append(attachedRSNames, rs.GetName())
			}
		default: // Deployment and ServiceAccount
			if objRef.Name == reconcilerName {
				return requeueRepoSyncRequest(obj, &rs)
			}
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to %s (%s) triggers a reconciliation for the RepoSync(s) (%s)",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, strings.Join(attachedRSNames, ", "))
	}
	return requests
}

func requeueRepoSyncRequest(obj client.Object, rs *v1beta1.RepoSync) []reconcile.Request {
	rsRef := client.ObjectKeyFromObject(rs)
	klog.Infof("Changes to %s (%s) triggers a reconciliation for the RepoSync (%s).",
		obj.GetObjectKind().GroupVersionKind().Kind, client.ObjectKeyFromObject(obj), rsRef)
	return []reconcile.Request{
		{
			NamespacedName: rsRef,
		},
	}
}

func (r *RepoSyncReconciler) populateContainerEnvs(ctx context.Context, rs *v1beta1.RepoSync, reconcilerName string) map[string][]corev1.EnvVar {
	result := map[string][]corev1.EnvVar{
		reconcilermanager.HydrationController: hydrationEnvs(rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, declared.Scope(rs.Namespace), reconcilerName, r.hydrationPollingPeriod.String()),
		reconcilermanager.Reconciler:          reconcilerEnvs(r.clusterName, rs.Name, reconcilerName, declared.Scope(rs.Namespace), rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, reposync.GetHelmBase(rs.Spec.Helm), r.reconcilerPollingPeriod.String(), rs.Spec.SafeOverride().StatusMode, v1beta1.GetReconcileTimeout(rs.Spec.SafeOverride().ReconcileTimeout), v1beta1.GetAPIServerTimeout(rs.Spec.SafeOverride().APIServerTimeout), enableRendering(rs.GetAnnotations())),
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
		result[reconcilermanager.HelmSync] = helmSyncEnvs(&rs.Spec.Helm.HelmBase, rs.Namespace, "", r.helmSyncVersionPollingPeriod.String())
	}
	return result
}

func (r *RepoSyncReconciler) validateSpec(ctx context.Context, rs *v1beta1.RepoSync, reconcilerName string) error {
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		return r.validateGitSpec(ctx, rs, reconcilerName)
	case v1beta1.OciSource:
		return validate.OciSpec(rs.Spec.Oci, rs)
	case v1beta1.HelmSource:
		if err := validate.HelmSpec(reposync.GetHelmBase(rs.Spec.Helm), rs); err != nil {
			return err
		}
		return validate.CheckValuesFileSourcesRefs(ctx, r.client, rs.Spec.Helm.ValuesFileSources, rs)
	default:
		return validate.InvalidSourceType(rs)
	}
}

func (r *RepoSyncReconciler) validateGitSpec(ctx context.Context, rs *v1beta1.RepoSync, reconcilerName string) error {
	if err := validate.GitSpec(rs.Spec.Git, rs); err != nil {
		return err
	}
	if err := r.validateCACertSecret(ctx, rs.Namespace, v1beta1.GetSecretName(rs.Spec.Git.CACertSecretRef)); err != nil {
		return err
	}
	return r.validateNamespaceSecret(ctx, rs, reconcilerName)
}

// validateNamespaceSecret verify that any necessary Secret is present before creating ConfigMaps and Deployments.
func (r *RepoSyncReconciler) validateNamespaceSecret(ctx context.Context, repoSync *v1beta1.RepoSync, reconcilerName string) error {
	var authType configsync.AuthType
	var namespaceSecretName string
	if repoSync.Spec.SourceType == string(v1beta1.GitSource) {
		authType = repoSync.Spec.Auth
		namespaceSecretName = v1beta1.GetSecretName(repoSync.Spec.SecretRef)
	} else if repoSync.Spec.SourceType == string(v1beta1.HelmSource) {
		authType = repoSync.Spec.Helm.Auth
		namespaceSecretName = v1beta1.GetSecretName(repoSync.Spec.Helm.SecretRef)
	}
	if SkipForAuth(authType) {
		// There is no Secret to check for the Config object.
		return nil
	}

	secretName := ReconcilerResourceName(reconcilerName, namespaceSecretName)
	if errs := validation.IsDNS1123Subdomain(secretName); errs != nil {
		return errors.Errorf("The managed secret name %q is invalid: %s. To fix it, update '.spec.git.secretRef.name'", secretName, strings.Join(errs, ", "))
	}

	secret, err := validateSecretExist(ctx,
		namespaceSecretName,
		repoSync.Namespace,
		r.client)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return errors.Errorf("Secret %s not found: create one to allow client authentication", namespaceSecretName)
		}
		return errors.Wrapf(err, "Secret %s get failed", namespaceSecretName)
	}
	return validateSecretData(authType, secret)
}

func (r *RepoSyncReconciler) validateNamespaceName(namespaceName string) error {
	if namespaceName == configsync.ControllerNamespace {
		return fmt.Errorf("RepoSync objects are not allowed in the %s namespace", configsync.ControllerNamespace)
	}
	return nil
}

func (r *RepoSyncReconciler) upsertRoleBinding(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) (client.ObjectKey, error) {
	rbRef := client.ObjectKey{
		Namespace: rsRef.Namespace,
		Name:      RepoSyncPermissionsName(),
	}
	childRB := &rbacv1.RoleBinding{}
	childRB.Name = rbRef.Name
	childRB.Namespace = rbRef.Namespace

	op, err := CreateOrUpdate(ctx, r.client, childRB, func() error {
		childRB.RoleRef = rolereference(RepoSyncPermissionsName(), "ClusterRole")
		childRB.Subjects = addSubject(childRB.Subjects, r.serviceAccountSubject(reconcilerRef))
		return nil
	})
	if err != nil {
		return rbRef, err
	}
	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, rbRef.String(),
			logFieldObjectKind, "RoleBinding",
			logFieldOperation, op)
	}
	return rbRef, nil
}

func (r *RepoSyncReconciler) updateStatus(ctx context.Context, currentRS, rs *v1beta1.RepoSync) (bool, error) {
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

func (r *RepoSyncReconciler) mutationsFor(ctx context.Context, rs *v1beta1.RepoSync, containerEnvs map[string][]corev1.EnvVar, newValuesFrom []v1beta1.ValuesFileSources) mutateFn {
	return func(obj client.Object) error {
		d, ok := obj.(*appsv1.Deployment)
		if !ok {
			return errors.Errorf("expected appsv1 Deployment, got: %T", obj)
		}
		reconcilerName := core.NsReconcilerName(rs.Namespace, rs.Name)

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
		// Update ServiceAccountName. eg. ns-reconciler-<namespace>
		templateSpec.ServiceAccountName = reconcilerName
		// The Deployment object fetched from the API server has the field defined.
		// Update DeprecatedServiceAccount to avoid discrepancy in equality check.
		templateSpec.DeprecatedServiceAccount = reconcilerName
		// Mutate secret.secretname to secret reference specified in RepoSync CR.
		// Secret reference is the name of the secret used by git-sync or helm-sync container to
		// authenticate with the git or helm repository using the authorization method specified
		// in the RepoSync CR.
		secretName := ReconcilerResourceName(reconcilerName, secretRefName)
		if useCACert(caCertSecretRefName) {
			caCertSecretRefName = ReconcilerResourceName(reconcilerName, caCertSecretRefName)
		}
		templateSpec.Volumes = filterVolumes(templateSpec.Volumes, auth, secretName, caCertSecretRefName, rs.Spec.SourceType, r.membership)
		var updatedContainers []corev1.Container
		// Mutate spec.Containers to update name, configmap references and volumemounts.
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
						container.Env = append(container.Env, helmSyncTokenAuthEnv(secretName)...)
					}
					mountConfigMapValuesFiles(templateSpec, &container, newValuesFrom)
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
					if authTypeToken(rs.Spec.Auth) {
						container.Env = append(container.Env, gitSyncTokenAuthEnv(secretName)...)
					}
					sRef := client.ObjectKey{Namespace: rs.Namespace, Name: v1beta1.GetSecretName(rs.Spec.SecretRef)}
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

func copyConfigMapsToCms(ctx context.Context, cl client.Client, rs *v1beta1.RepoSync) ([]v1beta1.ValuesFileSources, error) {
	if rs.Spec.SourceType != string(v1beta1.HelmSource) || rs.Spec.Helm == nil {
		return nil, nil
	}

	var newValuesFrom []v1beta1.ValuesFileSources
	for _, vf := range rs.Spec.Helm.ValuesFileSources {
		objRef := types.NamespacedName{
			Name:      vf.Name,
			Namespace: rs.Namespace,
		}

		newName, err := copyOneConfigMapToCms(ctx, cl, objRef)
		if err != nil {
			return nil, err
		}
		newValuesFrom = append(newValuesFrom, v1beta1.ValuesFileSources{
			Name:       newName,
			ValuesFile: vf.ValuesFile,
		})
	}

	return newValuesFrom, nil
}

func copyOneConfigMapToCms(ctx context.Context, cl client.Client, objRef types.NamespacedName) (string, error) {
	var cm corev1.ConfigMap
	if err := cl.Get(ctx, objRef, &cm); err != nil {
		return "", fmt.Errorf("failed to get referenced ConfigMap: %w", err)
	}

	newNamespace := configsync.ControllerNamespace
	newName := core.NsReconcilerPrefix + "-" + cm.Name

	var copiedCM corev1.ConfigMap
	if err := cl.Get(ctx, types.NamespacedName{Name: newName, Namespace: newNamespace}, &copiedCM); err != nil {
		if apierrors.IsNotFound(err) {
			cm.ObjectMeta = metav1.ObjectMeta{
				Namespace: newNamespace,
				Name:      newName,
			}
			if err := cl.Create(ctx, &cm); err != nil {
				return "", fmt.Errorf("failed to copy configmap: %w", err)
			}
		} else {
			return "", fmt.Errorf("unexpected error trying to get old copied configmap: %w", err)
		}
	} else {
		copiedCM.Data = cm.Data
		if err := cl.Update(ctx, &copiedCM); err != nil {
			return "", fmt.Errorf("error updating copied configmap: %w", err)
		}
	}

	return newName, nil
}
