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
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
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

// RepoSyncReconciler reconciles a RepoSync object.
type RepoSyncReconciler struct {
	reconcilerBase

	// lock ensures that the Reconcile method only runs one at a time.
	lock sync.Mutex

	// configMapWatches stores which namespaces where we are currently watching ConfigMaps
	configMapWatches map[string]bool

	controller *controller.Controller
}

const (
	// the following annotations are added to ConfigMaps that are copied from references
	// in spec.helm.valuesFileRefs to the config-management-system namespace, to keep track
	// of where the ConfigMap came from for cleanup
	repoSyncNameAnnotationKey      = configsync.ConfigSyncPrefix + "repo-sync-name"
	repoSyncNamespaceAnnotationKey = configsync.ConfigSyncPrefix + "repo-sync-namespace"

	// originalConfigMapNameAnnotationKey isn't used anywhere, but is added to ConfigMaps that are
	// copied from references in spec.helm.valuesFileRefs to the config-management-system namespace,
	// to keep track of where the ConfigMap came from, for debugging and troubleshooting
	originalConfigMapNameAnnotationKey = configsync.ConfigSyncPrefix + "original-configmap-name"
)

// NewRepoSyncReconciler returns a new RepoSyncReconciler.
func NewRepoSyncReconciler(clusterName string, reconcilerPollingPeriod, hydrationPollingPeriod time.Duration, client client.Client, watcher client.WithWatch, dynamicClient dynamic.Interface, log logr.Logger, scheme *runtime.Scheme) *RepoSyncReconciler {
	return &RepoSyncReconciler{
		reconcilerBase: reconcilerBase{
			loggingController: loggingController{
				log: log,
			},
			clusterName:             clusterName,
			client:                  client,
			dynamicClient:           dynamicClient,
			watcher:                 watcher,
			scheme:                  scheme,
			reconcilerPollingPeriod: reconcilerPollingPeriod,
			hydrationPollingPeriod:  hydrationPollingPeriod,
			syncKind:                configsync.RepoSyncKind,
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

	if rs.DeletionTimestamp.IsZero() {
		// Only validate RepoSync if it is not deleting. Otherwise, the validation
		// error will block the finalizer.
		if err := r.watchConfigMaps(rs); err != nil {
			r.logger(ctx).Error(err, "Error watching ConfigMaps")
			_, updateErr := r.updateSyncStatus(ctx, rs, reconcilerRef, func(syncObj *v1beta1.RepoSync) error {
				reposync.SetStalled(rs, "ConfigMapWatch", err)
				return nil
			})
			if updateErr != nil {
				r.logger(ctx).Error(updateErr, "Sync status update failed")
			}
			// Use the watch setup error for metric tagging.
			metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
			// Watch errors may be recoverable, so we retry (return the error)
			return controllerruntime.Result{}, err
		}

		// Validate the RepoSync after the ConfigMap watch is set up, so new
		// ConfigMaps can be validated.
		if err := r.validateRepoSync(ctx, rs, reconcilerRef.Name); err != nil {
			r.logger(ctx).Error(err, "Invalid RepoSync Spec")
			_, updateErr := r.updateSyncStatus(ctx, rs, reconcilerRef, func(syncObj *v1beta1.RepoSync) error {
				reposync.SetStalled(rs, "Validation", err)
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

func (r *RepoSyncReconciler) upsertManagedObjects(ctx context.Context, reconcilerRef types.NamespacedName, rs *v1beta1.RepoSync) error {
	rsRef := client.ObjectKeyFromObject(rs)
	r.logger(ctx).V(3).Info("Reconciling managed objects")

	// Create secret in config-management-system namespace using the
	// existing secret in the reposync.namespace.
	if _, err := r.upsertAuthSecret(ctx, rs, reconcilerRef); err != nil {
		return errors.Wrap(err, "upserting auth secret")
	}

	// Create secret in config-management-system namespace using the
	// existing secret in the reposync.namespace.
	if _, err := r.upsertCACertSecret(ctx, rs, reconcilerRef); err != nil {
		return errors.Wrap(err, "upserting CA cert secret")
	}

	labelMap := r.managedObjectLabelMap(rsRef)

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

	// Overwrite reconciler rolebinding.
	if err := r.configureRoleBindings(ctx, reconcilerRef, rsRef, rs.Spec.SafeOverride().RoleRefs); err != nil {
		return errors.Wrap(err, "upserting role binding")
	}

	if err := r.upsertHelmConfigMaps(ctx, rs, labelMap); err != nil {
		return errors.Wrap(err, "upserting helm config maps")
	}

	containerEnvs := r.populateContainerEnvs(ctx, rs, reconcilerRef.Name)
	mut := r.mutationsFor(ctx, rs, containerEnvs)

	// Upsert Namespace reconciler deployment.
	deployObj, op, err := r.upsertDeployment(ctx, reconcilerRef, labelMap, mut)
	if err != nil {
		return errors.Wrap(err, "upserting reconciler deployment")
	}
	rs.Status.Reconciler = reconcilerRef.Name

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
// - Convert any error into RepoSync status conditions
// - Update the RepoSync status
func (r *RepoSyncReconciler) setup(ctx context.Context, reconcilerRef types.NamespacedName, rs *v1beta1.RepoSync) error {
	err := r.upsertManagedObjects(ctx, reconcilerRef, rs)
	updated, updateErr := r.updateSyncStatus(ctx, rs, reconcilerRef, func(syncObj *v1beta1.RepoSync) error {
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

// teardown performs the following teardown steps:
// - Delete managed objects
// - Convert any error into RepoSync status conditions
// - Update the RepoSync status
func (r *RepoSyncReconciler) teardown(ctx context.Context, reconcilerRef types.NamespacedName, rs *v1beta1.RepoSync) error {
	rsRef := client.ObjectKeyFromObject(rs)
	err := r.deleteManagedObjects(ctx, reconcilerRef, rsRef)
	updated, updateErr := r.updateSyncStatus(ctx, rs, reconcilerRef, func(syncObj *v1beta1.RepoSync) error {
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

	if err != nil {
		err = errors.Wrap(err, stage)
	}
	return err // retry
}

// deleteManagedObjects deletes objects managed by the reconciler-manager for
// this RepoSync.
func (r *RepoSyncReconciler) deleteManagedObjects(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
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

	if err := r.deleteSecrets(ctx, reconcilerRef); err != nil {
		return errors.Wrap(err, "deleting secrets")
	}

	if err := r.cleanRoleBindings(ctx, reconcilerRef, rsRef); err != nil {
		return errors.Wrap(err, "deleting role binding")
	}

	if err := r.deleteHelmConfigMapCopies(ctx, rsRef, nil); err != nil {
		return errors.Wrap(err, "deleting helm config maps")
	}

	if err := r.deleteServiceAccount(ctx, reconcilerRef); err != nil {
		return errors.Wrap(err, "deleting service account")
	}

	return nil
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
		// Watch RoleBindings in all namespaces, because RoleBindings are created
		// in the namespace of the RepoSync. Only maps to existing RepoSyncs.
		Watches(&source.Kind{Type: &rbacv1.RoleBinding{}},
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
		len(rs.Spec.Helm.ValuesFileRefs) == 0 {
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
				return requeueRepoSyncRequest(secret, client.ObjectKeyFromObject(&rs))
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
						return requeueRepoSyncRequest(secret, client.ObjectKeyFromObject(&rs))
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

	// Use annotations/labels to map ConfigMap copies in config-management-system
	if objRef.Namespace == configsync.ControllerNamespace {
		rsRef := types.NamespacedName{}
		labels := obj.GetLabels()
		if labels != nil {
			rsRef.Name = labels[metadata.SyncNameLabel]
			rsRef.Namespace = labels[metadata.SyncNamespaceLabel]
		}
		// fallback to annotations, if labels not set
		// TODO: Eventually remove the annotations and use the labels for list filtering, to optimize cleanup.
		// We can't remove the annotations until v1.16.0 is no longer supported.
		annotations := obj.GetAnnotations()
		if annotations != nil {
			if len(rsRef.Name) == 0 {
				rsRef.Name = annotations[repoSyncNameAnnotationKey]
			}
			if len(rsRef.Namespace) == 0 {
				rsRef.Namespace = annotations[repoSyncNamespaceAnnotationKey]
			}
		}
		if len(rsRef.Name) > 0 && len(rsRef.Namespace) > 0 {
			return requeueRepoSyncRequest(obj, rsRef)
		}
		return nil
	}

	// For other namespaces, look up RepoSyncs in the same namespace and see
	// if any of them reference this ConfigMap.
	ctx := context.Background()
	repoSyncList := &v1beta1.RepoSyncList{}
	if err := r.client.List(ctx, repoSyncList, &client.ListOptions{Namespace: objRef.Namespace}); err != nil {
		klog.Errorf("failed to list RepoSyncs for %s (%s): %v",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, err)
		return nil
	}
	var requests []reconcile.Request
	var attachedRSNames []string
	for _, rs := range repoSyncList.Items {
		if rs.Spec.SourceType != string(v1beta1.HelmSource) {
			// we are only watching ConfigMaps to re-trigger helm-sync,
			// so we can ignore other source types
			continue
		}
		if rs.Spec.Helm == nil || len(rs.Spec.Helm.ValuesFileRefs) == 0 {
			continue
		}
		var referenced bool
		for _, vf := range rs.Spec.Helm.ValuesFileRefs {
			if vf.Name == objRef.Name {
				referenced = true
				break
			}
		}
		if !referenced {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&rs),
		})
		attachedRSNames = append(attachedRSNames, rs.GetName())
	}
	if len(requests) > 0 {
		klog.Infof("Changes to %s triggered a reconciliation for the RepoSync(s) (%s)",
			kinds.ObjectSummary(obj), strings.Join(attachedRSNames, ", "))
	}
	return requests
}

// mapObjectToRepoSync define a mapping from an object in 'config-management-system'
// namespace to a RepoSync to be reconciled.
func (r *RepoSyncReconciler) mapObjectToRepoSync(obj client.Object) []reconcile.Request {
	objRef := client.ObjectKeyFromObject(obj)

	// Ignore changes from resources without the ns-reconciler prefix or configsync.gke.io:ns-reconciler
	// because all the generated resources have the prefix.
	if !strings.HasPrefix(objRef.Name, core.NsReconcilerPrefix) && objRef.Name != RepoSyncBaseRoleBindingName {
		return nil
	}

	if err := r.addTypeInformationToObject(obj); err != nil {
		klog.Errorf("failed to lookup resource of object %T (%s): %v",
			obj, objRef, err)
		return nil
	}

	if objRef.Namespace != configsync.ControllerNamespace {
		switch obj.(type) {
		case *corev1.ServiceAccount, *appsv1.Deployment:
			// All Deployments and ServiceAccounts are in config-management-system
			return nil
		}
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
			if objRef.Name == RepoSyncBaseRoleBindingName && objRef.Namespace == rs.Namespace {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKeyFromObject(&rs),
				})
				attachedRSNames = append(attachedRSNames, rs.GetName())
			} else if strings.HasPrefix(objRef.Name, reconcilerName) && objRef.Namespace == rs.Namespace {
				// custom roleRef RoleBinding
				return requeueRepoSyncRequest(obj, client.ObjectKeyFromObject(&rs))
			}
		default: // Deployment and ServiceAccount
			if objRef.Name == reconcilerName {
				return requeueRepoSyncRequest(obj, client.ObjectKeyFromObject(&rs))
			}
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to %s (%s) triggers a reconciliation for the RepoSync(s) (%s)",
			obj.GetObjectKind().GroupVersionKind().Kind, objRef, strings.Join(attachedRSNames, ", "))
	}
	return requests
}

func requeueRepoSyncRequest(obj client.Object, rsRef types.NamespacedName) []reconcile.Request {
	klog.Infof("Changes to %s triggered a reconciliation for the RepoSync (%s).",
		kinds.ObjectSummary(obj), rsRef)
	return []reconcile.Request{
		{
			NamespacedName: rsRef,
		},
	}
}

func (r *RepoSyncReconciler) populateContainerEnvs(ctx context.Context, rs *v1beta1.RepoSync, reconcilerName string) map[string][]corev1.EnvVar {
	result := map[string][]corev1.EnvVar{
		reconcilermanager.HydrationController: hydrationEnvs(rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, declared.Scope(rs.Namespace), reconcilerName, r.hydrationPollingPeriod.String()),
		reconcilermanager.Reconciler:          reconcilerEnvs(r.clusterName, rs.Name, rs.Generation, reconcilerName, declared.Scope(rs.Namespace), rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, reposync.GetHelmBase(rs.Spec.Helm), r.reconcilerPollingPeriod.String(), rs.Spec.SafeOverride().StatusMode, v1beta1.GetReconcileTimeout(rs.Spec.SafeOverride().ReconcileTimeout), v1beta1.GetAPIServerTimeout(rs.Spec.SafeOverride().APIServerTimeout), enableRendering(rs.GetAnnotations())),
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
		})
		if enableAskpassSidecar(rs.Spec.SourceType, rs.Spec.Git.Auth) {
			result[reconcilermanager.GCENodeAskpassSidecar] = gceNodeAskPassSidecarEnvs(rs.Spec.GCPServiceAccountEmail)
		}
	case v1beta1.OciSource:
		result[reconcilermanager.OciSync] = ociSyncEnvs(rs.Spec.Oci.Image, rs.Spec.Oci.Auth, v1beta1.GetPeriod(rs.Spec.Oci.Period, configsync.DefaultReconcilerPollingPeriod).Seconds())
	case v1beta1.HelmSource:
		result[reconcilermanager.HelmSync] = helmSyncEnvs(&rs.Spec.Helm.HelmBase, rs.Namespace, "")
	}
	return result
}

func (r *RepoSyncReconciler) validateRepoSync(ctx context.Context, rs *v1beta1.RepoSync, reconcilerName string) error {
	if rs.Namespace == configsync.ControllerNamespace {
		return fmt.Errorf("RepoSync objects are not allowed in the %s namespace", configsync.ControllerNamespace)
	}

	if err := validation.IsDNS1123Subdomain(reconcilerName); err != nil {
		return fmt.Errorf("Invalid reconciler name %q: %s.", reconcilerName, strings.Join(err, ", "))
	}

	if err := r.validateSourceSpec(ctx, rs, reconcilerName); err != nil {
		return err
	}

	return r.validateValuesFileSourcesRefs(ctx, rs)
}

func (r *RepoSyncReconciler) validateSourceSpec(ctx context.Context, rs *v1beta1.RepoSync, reconcilerName string) error {
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		return r.validateGitSpec(ctx, rs, reconcilerName)
	case v1beta1.OciSource:
		return validate.OciSpec(rs.Spec.Oci, rs)
	case v1beta1.HelmSource:
		return validate.HelmSpec(reposync.GetHelmBase(rs.Spec.Helm), rs)
	default:
		return validate.InvalidSourceType(rs)
	}
}

// validateValuesFileSourcesRefs validates that the ConfigMaps specified in the RSync ValuesFileSources exist and have the
// specified data key.
func (r *RepoSyncReconciler) validateValuesFileSourcesRefs(ctx context.Context, rs *v1beta1.RepoSync) status.Error {
	if rs.Spec.SourceType != string(v1beta1.HelmSource) || rs.Spec.Helm == nil || len(rs.Spec.Helm.ValuesFileRefs) == 0 {
		return nil
	}
	return validate.ValuesFileRefs(ctx, r.client, rs, rs.Spec.Helm.ValuesFileRefs)
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

func (r *RepoSyncReconciler) listCurrentRoleRefs(ctx context.Context, rsRef types.NamespacedName) (map[v1beta1.RoleRefBase]client.Object, error) {
	rbList := rbacv1.RoleBindingList{}
	labelMap := r.managedObjectLabelMap(rsRef)
	opts := &client.ListOptions{}
	opts.Namespace = rsRef.Namespace
	opts.LabelSelector = client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(labelMap),
	}
	if err := r.client.List(ctx, &rbList, opts); err != nil {
		return nil, errors.Wrap(err, "listing RoleBindings")
	}
	currentRoleMap := make(map[v1beta1.RoleRefBase]client.Object)
	for idx, rb := range rbList.Items {
		roleRef := v1beta1.RoleRefBase{
			Kind: rb.RoleRef.Kind,
			Name: rb.RoleRef.Name,
		}
		currentRoleMap[roleRef] = &rbList.Items[idx]
	}
	return currentRoleMap, nil
}

func (r *RepoSyncReconciler) cleanRoleBindings(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
	currentRefMap, err := r.listCurrentRoleRefs(ctx, rsRef)
	if err != nil {
		return err
	}
	for _, roleBinding := range currentRefMap {
		if err := r.deleteRoleBinding(ctx, roleBinding.GetName(), roleBinding.GetNamespace()); err != nil {
			return errors.Wrap(err, "deleting RoleBinding")
		}
	}
	if err := r.deleteSharedRoleBinding(ctx, reconcilerRef, rsRef); err != nil {
		return errors.Wrap(err, "deleting base RoleBinding")
	}
	return nil
}

// configures RoleBindings for the RepoSync reconciler based on the RoleRefs
// provided by the RepoSync spec. Always creates a RoleBinding to the base
// ClusterRole for RepoSyncs to enable basic functionality.
func (r *RepoSyncReconciler) configureRoleBindings(ctx context.Context, reconcilerRef, rsRef types.NamespacedName, roleRefs []v1beta1.RoleRefBase) error {
	currentRefMap, err := r.listCurrentRoleRefs(ctx, rsRef)
	if err != nil {
		return err
	}
	// Add the base ClusterRole for basic ns reconciler functionality
	if err := r.upsertSharedRoleBinding(ctx, reconcilerRef, rsRef); err != nil {
		return err
	}
	declaredRoleRefs := roleRefs
	// Create RoleBindings which are declared but do not exist on the cluster
	for _, roleRef := range declaredRoleRefs {
		if _, ok := currentRefMap[roleRef]; !ok {
			// we need to call a separate create method here for generateName
			if _, err := r.createRBACBinding(ctx, reconcilerRef, rsRef, roleRef, rsRef.Namespace); err != nil {
				return errors.Wrap(err, "creating RoleBinding")
			}
		}
	}
	// convert to map for lookup
	declaredRefMap := make(map[v1beta1.RoleRefBase]bool)
	for _, roleRef := range declaredRoleRefs {
		declaredRefMap[roleRef] = true
	}
	// For existing RoleBindings:
	// - if they are declared in roleRefs, update
	// - if they are no longer declared in roleRefs, delete
	for roleRef, roleBinding := range currentRefMap {
		if _, ok := declaredRefMap[roleRef]; ok { // update
			if err := r.updateRBACBinding(ctx, reconcilerRef, roleBinding); err != nil {
				return errors.Wrap(err, "updating RoleBinding")
			}
		} else { // Clean up any RoleRefs created previously that are no longer declared
			if err := r.cleanup(ctx, roleBinding); err != nil {
				return errors.Wrap(err, "deleting RoleBinding")
			}
		}
	}
	return nil
}

func (r *RepoSyncReconciler) upsertSharedRoleBinding(ctx context.Context, reconcilerRef, rsRef types.NamespacedName) error {
	rbRef := client.ObjectKey{
		Namespace: rsRef.Namespace,
		Name:      RepoSyncBaseClusterRoleName,
	}
	childRB := &rbacv1.RoleBinding{}
	childRB.Name = rbRef.Name
	childRB.Namespace = rbRef.Namespace

	op, err := CreateOrUpdate(ctx, r.client, childRB, func() error {
		childRB.RoleRef = rolereference(RepoSyncBaseClusterRoleName, "ClusterRole")
		childRB.Subjects = addSubject(childRB.Subjects, r.serviceAccountSubject(reconcilerRef))
		return nil
	})
	if err != nil {
		return err
	}
	if op != controllerutil.OperationResultNone {
		r.logger(ctx).Info("Managed object upsert successful",
			logFieldObjectRef, rbRef.String(),
			logFieldObjectKind, "RoleBinding",
			logFieldOperation, op)
	}
	return nil
}

func (r *RepoSyncReconciler) updateSyncStatus(ctx context.Context, rs *v1beta1.RepoSync, reconcilerRef types.NamespacedName, updateFn func(*v1beta1.RepoSync) error) (bool, error) {
	// Always set the reconciler and observedGeneration when updating sync status
	updateFn2 := func(syncObj *v1beta1.RepoSync) error {
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

func (r *RepoSyncReconciler) mutationsFor(ctx context.Context, rs *v1beta1.RepoSync, containerEnvs map[string][]corev1.EnvVar) mutateFn {
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

		// Add sync-generation label
		core.SetLabel(&d.ObjectMeta, metadata.SyncGenerationLabel, fmt.Sprint(rs.GetGeneration()))
		core.SetLabel(&d.Spec.Template, metadata.SyncGenerationLabel, fmt.Sprint(rs.GetGeneration()))

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
		// Mutate spec.Containers to update name, configmap references and volumemounts.
		for _, container := range templateSpec.Containers {
			addContainer := true
			switch container.Name {
			case reconcilermanager.Reconciler:
				container.Env = append(container.Env, containerEnvs[container.Name]...)
			case reconcilermanager.HydrationController:
				if !enableRendering(rs.GetAnnotations()) {
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
					injectFWICredsToContainer(&container, injectFWICreds)
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
					if authTypeToken(rs.Spec.Auth) {
						container.Env = append(container.Env, gitSyncTokenAuthEnv(secretName)...)
					}
					sRef := client.ObjectKey{Namespace: rs.Namespace, Name: v1beta1.GetSecretName(rs.Spec.SecretRef)}
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
				// Common mutations for all added containers
				mutateContainerResource(&container, containerResources)
				mutateContainerLogLevel(&container, containerLogLevels)
				updatedContainers = append(updatedContainers, container)
			}
		}

		templateSpec.Containers = updatedContainers
		return nil
	}
}

func enableAskpassSidecar(sourceType string, auth configsync.AuthType) bool {
	if v1beta1.SourceType(sourceType) == v1beta1.GitSource &&
		(auth == configsync.AuthGCPServiceAccount || auth == configsync.AuthGCENode) {
		return true
	}
	return false
}
