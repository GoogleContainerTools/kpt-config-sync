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
	// repoSyncs is a cache of the reconciled RepoSync objects.
	repoSyncs map[types.NamespacedName]struct{}

	lock sync.Mutex
}

// NewRepoSyncReconciler returns a new RepoSyncReconciler.
func NewRepoSyncReconciler(clusterName string, reconcilerPollingPeriod, hydrationPollingPeriod time.Duration, client client.Client, dynamicClient dynamic.Interface, log logr.Logger, scheme *runtime.Scheme) *RepoSyncReconciler {
	return &RepoSyncReconciler{
		reconcilerBase: reconcilerBase{
			loggingController: loggingController{
				log: log,
			},
			clusterName:             clusterName,
			client:                  client,
			dynamicClient:           dynamicClient,
			scheme:                  scheme,
			reconcilerPollingPeriod: reconcilerPollingPeriod,
			hydrationPollingPeriod:  hydrationPollingPeriod,
			syncKind:                configsync.RepoSyncKind,
		},
		repoSyncs: make(map[types.NamespacedName]struct{}),
	}
}

// +kubebuilder:rbac:groups=configsync.gke.io,resources=reposyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=configsync.gke.io,resources=reposyncs/status,verbs=get;update;patch

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
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		if apierrors.IsNotFound(err) {
			if _, ok := r.repoSyncs[rsRef]; !ok {
				r.logger(ctx).Error(err, "Sync object not managed by this reconciler-manager")
				// return `controllerruntime.Result{}, nil` here to make sure the request will not be requeued.
				return controllerruntime.Result{}, nil
			}
			// Namespace controller resources are cleaned up when reposync no longer present.
			//
			// Note: Update cleanup resources in cleanupNSControllerResources(...) when
			// resources created by namespace controller changes.
			cleanupErr := r.cleanupNSControllerResources(ctx, rsRef, reconcilerRef)
			// if cleanupErr != nil, the request will be requeued.
			return controllerruntime.Result{}, cleanupErr
		}
		return controllerruntime.Result{}, status.APIServerError(err, "failed to get RepoSync")
	}

	if !rs.DeletionTimestamp.IsZero() {
		// Deletion requested.
		// Cleanup is handled above, after the RepoSync is NotFound.
		r.logger(ctx).V(3).Info("Sync deletion timestamp detected")
	}

	currentRS := rs.DeepCopy()

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

	r.repoSyncs[rsRef] = struct{}{}
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

	// Create secret in config-management-system namespace using the
	// existing secret in the reposync.namespace.
	if sRef, err := r.upsertAuthSecret(ctx, rs, reconcilerRef); err != nil {
		var authType configsync.AuthType
		if rs.Spec.SourceType == string(v1beta1.GitSource) {
			authType = rs.Spec.Auth
		} else if rs.Spec.SourceType == string(v1beta1.HelmSource) {
			authType = rs.Spec.Helm.Auth
		}
		r.logger(ctx).Error(err, "Managed object upsert failed",
			logFieldObjectRef, sRef.String(),
			logFieldObjectKind, "Secret",
			"type", "auth",
			"auth", authType)
		reposync.SetStalled(rs, "Secret", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Use the upsert error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, errors.Wrap(err, "Secret reconcile failed")
	}

	// Create secret in config-management-system namespace using the
	// existing secret in the reposync.namespace.
	if sRef, err := r.upsertCACertSecret(ctx, rs, reconcilerRef); err != nil {
		r.logger(ctx).Error(err, "Managed object upsert failed",
			logFieldObjectRef, sRef.String(),
			logFieldObjectKind, "Secret",
			"type", "caCert")
		reposync.SetStalled(rs, "Secret", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Use the upsert error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, errors.Wrap(err, "Secret reconcile failed")
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
	}
	if saRef, err := r.upsertServiceAccount(ctx, reconcilerRef, auth, gcpSAEmail, labelMap); err != nil {
		r.logger(ctx).Error(err, "Managed object upsert failed",
			logFieldObjectRef, saRef.String(),
			logFieldObjectKind, "ServiceAccount",
			"sourceType", rs.Spec.SourceType,
			"auth", auth)
		reposync.SetStalled(rs, "ServiceAccount", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Use the upsert error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, errors.Wrap(err, "ServiceAccount reconcile failed")
	}

	// Overwrite reconciler rolebinding.
	if rbRef, err := r.upsertRoleBinding(ctx, reconcilerRef, rsRef); err != nil {
		r.logger(ctx).Error(err, "Managed object upsert failed",
			logFieldObjectRef, rbRef.String(),
			logFieldObjectKind, "RoleBinding")
		reposync.SetStalled(rs, "RoleBinding", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Use the upsert error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, errors.Wrap(err, "RoleBinding reconcile failed")
	}

	containerEnvs := r.populateContainerEnvs(ctx, rs, reconcilerRef.Name)
	mut := r.mutationsFor(ctx, rs, containerEnvs)

	// Upsert Namespace reconciler deployment.
	deployObj, op, err := r.upsertDeployment(ctx, reconcilerRef, labelMap, mut)
	if err != nil {
		r.logger(ctx).Error(err, "Managed object get failed",
			logFieldObjectRef, reconcilerRef.String(),
			logFieldObjectKind, "Deployment")
		reposync.SetStalled(rs, "Deployment", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Use the upsert error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, errors.Wrap(err, "Deployment reconcile failed")
	}
	rs.Status.Reconciler = reconcilerRef.Name

	// Get the latest deployment to check the status.
	// For other operations, upsertDeployment will have returned the latest already.
	if op == controllerutil.OperationResultNone {
		deployObj, err = r.deployment(ctx, reconcilerRef)
		if err != nil {
			r.logger(ctx).Error(err, "Managed object get failed",
				logFieldObjectRef, reconcilerRef.String(),
				logFieldObjectKind, "Deployment")
			reposync.SetStalled(rs, "Deployment", err)
			// Get errors should always trigger retry (return error),
			// even if status update is successful.
			_, updateErr := r.updateStatus(ctx, currentRS, rs)
			if updateErr != nil {
				r.logger(ctx).Error(updateErr, "Sync status update failed")
			}
			// Use the deployment get error for metric tagging.
			metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
			return controllerruntime.Result{}, err
		}
	}

	result, err := kstatus.Compute(deployObj)
	if err != nil {
		r.logger(ctx).Error(err, "Managed object status check failed",
			logFieldObjectRef, reconcilerRef.String(),
			logFieldObjectKind, "Deployment")
		reposync.SetStalled(rs, "Deployment", err)
		// Get errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Use the compute error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, err
	}

	r.logger(ctx).V(3).Info("Reconciler status",
		logFieldObjectRef, reconcilerRef.String(),
		logFieldObjectKind, "Deployment",
		logFieldResourceVersion, deployObj.GetResourceVersion(),
		"status", result.Status,
		"message", result.Message)

	// Update RepoSync status based on reconciler deployment condition result.
	switch result.Status {
	case kstatus.InProgressStatus:
		// inProgressStatus indicates that the deployment is not yet
		// available. Hence update the Reconciling status condition.
		reposync.SetReconciling(rs, "Deployment", result.Message)
		// Clear Stalled condition.
		reposync.ClearCondition(rs, v1beta1.RepoSyncStalled)
	case kstatus.FailedStatus, kstatus.TerminatingStatus:
		// statusFailed indicates that the deployment failed to reconcile. Update
		// Reconciling status condition with appropriate message specifying the
		// reason for failure.
		reposync.SetReconciling(rs, "Deployment", result.Message)
		// Set Stalled condition with the deployment statusFailed.
		reposync.SetStalled(rs, "Deployment", errors.New(string(result.Status)))
	case kstatus.CurrentStatus:
		// currentStatus indicates that the deployment is available, which qualifies
		// to clear the Reconciling status condition in RepoSync.
		reposync.ClearCondition(rs, v1beta1.RepoSyncReconciling)
		// Since there were no errors, we can clear any previous Stalled condition.
		reposync.ClearCondition(rs, v1beta1.RepoSyncStalled)
	default:
		// Shouldn't happen, unless kstatus.Compute impl changes
		err := errors.Errorf("invalid deployment status: %v: %s", result.Status, result.Message)
		reposync.SetStalled(rs, "Deployment", err)
		// Get errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			r.logger(ctx).Error(updateErr, "Sync status update failed")
		}
		// Use the status error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, err
	}

	updated, err := r.updateStatus(ctx, currentRS, rs)
	// Use the status update error for metric tagging, if no other errors.
	metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
	if err != nil {
		return controllerruntime.Result{}, err
	}

	if updated && result.Status == kstatus.CurrentStatus {
		r.logger(ctx).Info("Sync object reconcile successful")
	}
	return controllerruntime.Result{}, nil
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
	return controllerBuilder.Complete(r)
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
		klog.Errorf("failed to add type information to object (name: %s, namespace: %s): %v", objRef.Name, obj.GetNamespace(), err)
		return nil
	}

	allRepoSyncs := &v1beta1.RepoSyncList{}
	if err := r.client.List(context.Background(), allRepoSyncs); err != nil {
		klog.Errorf("failed to list all RepoSyncs for object (name: %s, namespace: %s): %v", objRef.Name, obj.GetNamespace(), err)
		return nil
	}

	// Most of the resources are mapped to a single RepoSync object except RoleBinding.
	// All RepoSync objects share the same RoleBinding object, so requeue all RepoSync objects the RoleBinding is changed.
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
		reconcilermanager.Reconciler:          reconcilerEnvs(r.clusterName, rs.Name, reconcilerName, declared.Scope(rs.Namespace), rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, reposync.GetHelmBase(rs.Spec.Helm), r.reconcilerPollingPeriod.String(), rs.Spec.SafeOverride().StatusMode, v1beta1.GetReconcileTimeout(rs.Spec.SafeOverride().ReconcileTimeout), v1beta1.GetAPIServerTimeout(rs.Spec.SafeOverride().APIServerTimeout)),
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
		result[reconcilermanager.HelmSync] = helmSyncEnvs(&rs.Spec.Helm.HelmBase, rs.Namespace, "")
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
		return validate.HelmSpec(reposync.GetHelmBase(rs.Spec.Helm), rs)
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

	op, err := controllerruntime.CreateOrUpdate(ctx, r.client, childRB, func() error {
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
				container.Env = append(container.Env, containerEnvs[container.Name]...)
				if rs.Spec.SafeOverride().EnableShellInRendering == nil || !*rs.Spec.SafeOverride().EnableShellInRendering {
					container.Image = strings.ReplaceAll(container.Image, reconcilermanager.HydrationControllerWithShell, reconcilermanager.HydrationController)
				} else {
					container.Image = strings.ReplaceAll(container.Image, reconcilermanager.HydrationController+":", reconcilermanager.HydrationControllerWithShell+":")
				}
				mutateContainerResource(&container, rs.Spec.Override)
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
