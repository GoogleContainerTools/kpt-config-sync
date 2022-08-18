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
	"k8s.io/klog/v2"
	"kpt.dev/configsync/pkg/api/configmanagement"
	"kpt.dev/configsync/pkg/api/configsync"
	"kpt.dev/configsync/pkg/api/configsync/v1beta1"
	hubv1 "kpt.dev/configsync/pkg/api/hub/v1"
	"kpt.dev/configsync/pkg/core"
	"kpt.dev/configsync/pkg/declared"
	"kpt.dev/configsync/pkg/diff"
	"kpt.dev/configsync/pkg/metadata"
	"kpt.dev/configsync/pkg/metrics"
	"kpt.dev/configsync/pkg/reconcilermanager"
	"kpt.dev/configsync/pkg/rootsync"
	"kpt.dev/configsync/pkg/status"
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

const kindRootSync = "RootSync"

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
}

// NewRootSyncReconciler returns a new RootSyncReconciler.
func NewRootSyncReconciler(clusterName string, reconcilerPollingPeriod, hydrationPollingPeriod time.Duration, client client.Client, log logr.Logger, scheme *runtime.Scheme) *RootSyncReconciler {
	return &RootSyncReconciler{
		reconcilerBase: reconcilerBase{
			clusterName:             clusterName,
			client:                  client,
			log:                     log,
			scheme:                  scheme,
			reconcilerPollingPeriod: reconcilerPollingPeriod,
			hydrationPollingPeriod:  hydrationPollingPeriod,
			syncKind:                kindRootSync,
		},
	}
}

// +kubebuilder:rbac:groups=configsync.gke.io,resources=rootsyncs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=configsync.gke.io,resources=rootsyncs/status,verbs=get;update;patch

// Reconcile the RootSync resource.
func (r *RootSyncReconciler) Reconcile(ctx context.Context, req controllerruntime.Request) (controllerruntime.Result, error) {
	rsRef := req.NamespacedName
	log := r.log.WithValues("rootsync", rsRef)
	start := time.Now()
	var err error
	rs := &v1beta1.RootSync{}
	reconcilerRef := types.NamespacedName{
		Namespace: configmanagement.ControllerNamespace,
		Name:      core.RootReconcilerName(rsRef.Name),
	}

	if err = r.client.Get(ctx, rsRef, rs); err != nil {
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		if apierrors.IsNotFound(err) {
			return controllerruntime.Result{}, r.deleteClusterRoleBinding(ctx, reconcilerRef.Namespace)
		}
		return controllerruntime.Result{}, status.APIServerError(err, "failed to get RootSync")
	}

	currentRS := rs.DeepCopy()

	if err = r.validateNamespaceName(rsRef.Namespace); err != nil {
		log.Error(err, "Namespace invalid",
			logFieldObject, rsRef.String(),
			logFieldKind, r.syncKind)
		rootsync.SetStalled(rs, "Validation", err)
		// Validation errors should not trigger retry (return error),
		// unless the status update also fails.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		// Use the validation error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, updateErr
	}

	owRefs := ownerReference(
		rs.GroupVersionKind().Kind,
		rs.Name,
		rs.UID,
	)

	if errs := validation.IsDNS1123Subdomain(reconcilerRef.Name); errs != nil {
		err = errors.Errorf("Invalid reconciler name %q: %s.", reconcilerRef.Name, strings.Join(errs, ", "))
		log.Error(err, "Name or namespace invalid",
			logFieldObject, rsRef.String(),
			logFieldKind, r.syncKind)
		rootsync.SetStalled(rs, "Validation", err)
		// Validation errors should not trigger retry (return error),
		// unless the status update also fails.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		// Use the validation error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, updateErr
	}

	if err = r.validateSpec(ctx, rs, log); err != nil {
		log.Error(err, "Spec invalid",
			logFieldObject, rsRef.String(),
			logFieldKind, r.syncKind)
		rootsync.SetStalled(rs, "Validation", err)
		// Validation errors should not trigger retry (return error),
		// unless the status update also fails.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		// Use the validation error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, updateErr
	}

	rootsyncLabelMap := map[string]string{
		metadata.SyncNamespaceLabel: rs.Namespace,
		metadata.SyncNameLabel:      rs.Name,
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
	if saRef, err := r.upsertServiceAccount(ctx, reconcilerRef, auth, gcpSAEmail, rootsyncLabelMap, owRefs); err != nil {
		log.Error(err, "Managed object upsert failed",
			logFieldObject, saRef.String(),
			logFieldKind, "ServiceAccount",
			"sourceType", rs.Spec.SourceType,
			"auth", auth)
		rootsync.SetStalled(rs, "ServiceAccount", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			log.Error(updateErr, "Object status update failed",
				logFieldObject, rsRef.String(),
				logFieldKind, r.syncKind)
		}
		// Use the upsert error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, errors.Wrap(err, "ServiceAccount reconcile failed")
	}

	// Overwrite reconciler clusterrolebinding.
	if crbRef, err := r.upsertClusterRoleBinding(ctx, rs.Namespace, reconcilerRef.Namespace); err != nil {
		log.Error(err, "Managed object upsert failed",
			logFieldObject, crbRef.String(),
			logFieldKind, "ClusterRoleBinding")
		rootsync.SetStalled(rs, "ClusterRoleBinding", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			log.Error(updateErr, "Object status update failed",
				logFieldObject, rsRef.String(),
				logFieldKind, r.syncKind)
		}
		// Use the upsert error for metric tagging.
		metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
		return controllerruntime.Result{}, errors.Wrap(err, "ClusterRoleBinding reconcile failed")
	}

	containerEnvs := r.populateContainerEnvs(ctx, rs, reconcilerRef.Name)
	mut := r.mutationsFor(ctx, rs, containerEnvs)

	// Upsert Root reconciler deployment.
	deployObj, op, err := r.upsertDeployment(ctx, reconcilerRef, rootsyncLabelMap, mut)
	if err != nil {
		log.Error(err, "Managed object upsert failed",
			logFieldObject, reconcilerRef.String(),
			logFieldKind, "Deployment")
		rootsync.SetStalled(rs, "Deployment", err)
		// Upsert errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			log.Error(updateErr, "Object status update failed",
				logFieldObject, rsRef.String(),
				logFieldKind, r.syncKind)
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
			log.Error(err, "Managed object get failed",
				logFieldObject, reconcilerRef.String(),
				logFieldKind, "Deployment")
			rootsync.SetStalled(rs, "Deployment", err)
			// Get errors should always trigger retry (return error),
			// even if status update is successful.
			_, updateErr := r.updateStatus(ctx, currentRS, rs)
			if updateErr != nil {
				log.Error(updateErr, "Object status update failed",
					logFieldObject, rsRef.String(),
					logFieldKind, r.syncKind)
			}
			return controllerruntime.Result{}, err
		}
	}

	result, err := computeDeploymentStatus(deployObj)
	if err != nil {
		log.Error(err, "Managed object status check failed",
			logFieldObject, reconcilerRef.String(),
			logFieldKind, "Deployment")
		rootsync.SetStalled(rs, "Deployment", err)
		// Get errors should always trigger retry (return error),
		// even if status update is successful.
		_, updateErr := r.updateStatus(ctx, currentRS, rs)
		if updateErr != nil {
			log.Error(updateErr, "Object status update failed",
				logFieldObject, rsRef.String(),
				logFieldKind, r.syncKind)
		}
		return controllerruntime.Result{}, err
	}

	// Update RootSync status based on reconciler deployment condition result.
	switch result.Status {
	case kstatus.InProgressStatus:
		// inProgressStatus indicates that the deployment is not yet
		// available. Hence update the Reconciling status condition.
		rootsync.SetReconciling(rs, "Deployment", result.Message)
		// Clear Stalled condition.
		rootsync.ClearCondition(rs, v1beta1.RootSyncStalled)
	case kstatus.FailedStatus:
		// statusFailed indicates that the deployment failed to reconcile. Update
		// Reconciling status condition with appropriate message specifying the
		// reason for failure.
		rootsync.SetReconciling(rs, "Deployment", result.Message)
		// Set Stalled condition with the deployment statusFailed.
		rootsync.SetStalled(rs, "Deployment", errors.New(string(result.Status)))
	case kstatus.CurrentStatus:
		// currentStatus indicates that the deployment is available, which qualifies
		// to clear the Reconciling status condition in RootSync.
		rootsync.ClearCondition(rs, v1beta1.RootSyncReconciling)
		// Since there were no errors, we can clear any previous Stalled condition.
		rootsync.ClearCondition(rs, v1beta1.RootSyncStalled)
	}

	updated, err := r.updateStatus(ctx, currentRS, rs)
	// Use the status update error for metric tagging, if no other errors.
	metrics.RecordReconcileDuration(ctx, metrics.StatusTagKey(err), start)
	if err != nil {
		return controllerruntime.Result{}, err
	}

	if updated && result.Status == kstatus.CurrentStatus {
		r.log.Info("Sync object successfully reconciled",
			logFieldObject, rsRef.String(),
			logFieldKind, r.syncKind)
	}
	return controllerruntime.Result{}, nil
}

// SetupWithManager registers RootSync controller with reconciler-manager.
func (r *RootSyncReconciler) SetupWithManager(mgr controllerruntime.Manager, watchFleetMembership bool) error {
	// Index the `gitSecretRefName` field, so that we will be able to lookup RootSync be a referenced `SecretRef` name.
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1beta1.RootSync{}, gitSecretRefField, func(rawObj client.Object) []string {
		rs := rawObj.(*v1beta1.RootSync)
		if rs.Spec.Git == nil || rs.Spec.Git.SecretRef.Name == "" {
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
		// Reconcile RootSync in all namespaces.
		// Reconcile adds an error condition if not in the controller namespace.
		For(&v1beta1.RootSync{},
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		// Trigger RootSync reconcile when a managed Deployment changes.
		// OwnerReferences added by reconcile, enabling garbage collection.
		Owns(&appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Namespace: configmanagement.ControllerNamespace}},
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		// Trigger RootSync reconcile when a managed ServiceAccount changes.
		// OwnerReferences added by reconcile, enabling garbage collection.
		Owns(&corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{Namespace: configmanagement.ControllerNamespace}},
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		// Trigger RootSync reconcile when a user-managed Secret changes.
		Watches(&source.Kind{Type: &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: configmanagement.ControllerNamespace}}},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToRootSyncs),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{})).
		// Trigger RootSync reconcile when a managed ClusterRoleBinding changes.
		// ClusterRoleBindings are shared between RootSync, not owned by any one.
		Watches(&source.Kind{Type: &rbacv1.ClusterRoleBinding{}},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectToRootSync),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}))

	if watchFleetMembership {
		// Trigger RootSync reconcile when a managed (Fleet) Membership changes.
		controllerBuilder.Watches(&source.Kind{Type: &hubv1.Membership{}},
			handler.EnqueueRequestsFromMapFunc(r.mapMembershipToRootSyncs()),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}))
	}
	return controllerBuilder.Complete(r)
}

// mapObjectToRootSync define a mapping from an object in 'config-management-system'
// namespace to a RootSync to be reconciled.
func (r *RootSyncReconciler) mapObjectToRootSync(obj client.Object) []reconcile.Request {
	// Ignore changes from resources without the root-reconciler prefix or configsync.gke.io:root-reconciler
	// because all the generated resources have the prefix.
	roleBindingName := RootSyncPermissionsName()
	if !strings.HasPrefix(obj.GetName(), core.RootReconcilerPrefix) && obj.GetName() != roleBindingName {
		return nil
	}

	if err := r.addTypeInformationToObject(obj); err != nil {
		klog.Errorf("failed to add type information to object (name: %s, namespace: %s): %v", obj.GetName(), obj.GetNamespace(), err)
		return nil
	}

	allRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(context.Background(), allRootSyncs); err != nil {
		klog.Error("failed to list all RootSyncs for object (name: %s, namespace: %s): %v", obj.GetName(), obj.GetNamespace(), err)
		return nil
	}

	// Most of the resources are mapped to a single RootSync object except ClusterRoleBinding.
	// All RootSync objects share the same ClusterRoleBinding object, so requeue all RootSync objects if the ClusterRoleBinding is changed.
	// For other resources, requeue the mapping RootSync object and then return.
	var requests []reconcile.Request
	var rsRefs []string
	for _, rs := range allRootSyncs.Items {
		reconcilerName := core.RootReconcilerName(rs.GetName())
		switch obj.(type) {
		case *rbacv1.ClusterRoleBinding:
			if obj.GetName() == roleBindingName {
				rsRef := client.ObjectKeyFromObject(&rs)
				rsRefs = append(rsRefs, rsRef.String())
				requests = append(requests, reconcile.Request{NamespacedName: rsRef})
			}
		default: // Deployment and ServiceAccount
			if obj.GetName() == reconcilerName {
				return requeueRootSyncRequest(obj, &rs)
			}
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to %s (%s) enqueued reconciliation for RootSync object(s): %s",
			obj.GetObjectKind().GroupVersionKind().Kind, client.ObjectKeyFromObject(obj), strings.Join(rsRefs, ", "))
	}
	return requests
}

func requeueRootSyncRequest(obj client.Object, rs *v1beta1.RootSync) []reconcile.Request {
	rsRef := client.ObjectKeyFromObject(rs)
	klog.Infof("Changes to %s (%s) enqueued reconciliation for RootSync object: %s",
		obj.GetObjectKind().GroupVersionKind().Kind, client.ObjectKeyFromObject(obj), rsRef)
	return []reconcile.Request{
		{
			NamespacedName: rsRef,
		},
	}
}

func (r *RootSyncReconciler) mapMembershipToRootSyncs() func(client.Object) []reconcile.Request {
	return func(o client.Object) []reconcile.Request {
		// Clear the membership if the cluster is unregistered
		if err := r.client.Get(context.Background(), types.NamespacedName{Name: fleetMembershipName}, &hubv1.Membership{}); err != nil {
			if apierrors.IsNotFound(err) {
				klog.Infof("Clear membership cache because %v", err)
				r.membership = nil
				return r.requeueAllRootSyncs()
			}
			klog.Errorf("failed to get membership: %v", err)
			return nil
		}

		m, isMembership := o.(*hubv1.Membership)
		if !isMembership {
			klog.Error("object is not a type of membership, gvk ", o.GetObjectKind().GroupVersionKind())
			return nil
		}
		if m.Name != fleetMembershipName {
			klog.Error("membership is %s, not 'membership'", m.Name)
			return nil
		}
		r.membership = m
		return r.requeueAllRootSyncs()
	}
}

func (r *RootSyncReconciler) requeueAllRootSyncs() []reconcile.Request {
	allRootSyncs := &v1beta1.RootSyncList{}
	if err := r.client.List(context.Background(), allRootSyncs); err != nil {
		klog.Error("failed to list all RootSyncs: %v", err)
		return nil
	}

	requests := make([]reconcile.Request, len(allRootSyncs.Items))
	for i, rs := range allRootSyncs.Items {
		requests[i] = reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&rs),
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to Membership enqueued reconciliation for %d RootSync objects", len(allRootSyncs.Items))
	}
	return requests
}

// mapSecretToRootSyncs define a mapping from the Secret object to its attached
// RootSync objects via the `spec.git.secretRef.name` field .
// The update to the Secret object will trigger a reconciliation of the RootSync objects.
func (r *RootSyncReconciler) mapSecretToRootSyncs(secret client.Object) []reconcile.Request {
	// Ignore secret in other namespaces because the RootSync's git secret MUST
	// exist in the config-management-system namespace.
	if secret.GetNamespace() != configsync.ControllerNamespace {
		return nil
	}

	// Ignore secret starts with ns-reconciler prefix because those are for RepoSync objects.
	if strings.HasPrefix(secret.GetName(), core.NsReconcilerPrefix+"-") {
		return nil
	}

	attachedRootSyncs := &v1beta1.RootSyncList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(gitSecretRefField, secret.GetName()),
		Namespace:     secret.GetNamespace(),
	}
	if err := r.client.List(context.Background(), attachedRootSyncs, listOps); err != nil {
		klog.Errorf("failed to list attached RootSyncs for secret (name: %s, namespace: %s): %v", secret.GetName(), secret.GetNamespace(), err)
		return nil
	}

	requests := make([]reconcile.Request, len(attachedRootSyncs.Items))
	rsRefs := make([]string, len(attachedRootSyncs.Items))
	for i, rs := range attachedRootSyncs.Items {
		rsRef := client.ObjectKeyFromObject(&rs)
		rsRefs[i] = rsRef.String()
		requests[i] = reconcile.Request{
			NamespacedName: rsRef,
		}
	}
	if len(requests) > 0 {
		klog.Infof("Changes to Secret (%s) enqueued reconciliation for RootSync object(s): %s",
			client.ObjectKeyFromObject(secret), strings.Join(rsRefs, ", "))
	}
	return requests
}

func (r *RootSyncReconciler) populateContainerEnvs(ctx context.Context, rs *v1beta1.RootSync, reconcilerName string) map[string][]corev1.EnvVar {
	result := map[string][]corev1.EnvVar{
		reconcilermanager.HydrationController: hydrationEnvs(rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, declared.RootReconciler, reconcilerName, r.hydrationPollingPeriod.String()),
		reconcilermanager.Reconciler:          append(reconcilerEnvs(r.clusterName, rs.Name, reconcilerName, declared.RootReconciler, rs.Spec.SourceType, rs.Spec.Git, rs.Spec.Oci, rs.Spec.Helm, r.reconcilerPollingPeriod.String(), rs.Spec.Override.StatusMode, v1beta1.GetReconcileTimeout(rs.Spec.Override.ReconcileTimeout)), sourceFormatEnv(rs.Spec.SourceFormat)),
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
			depth:           rs.Spec.Override.GitSyncDepth,
			noSSLVerify:     rs.Spec.Git.NoSSLVerify,
			caCertSecretRef: rs.Spec.Git.CACertSecretRef.Name,
		})
	case v1beta1.OciSource:
		result[reconcilermanager.OciSync] = ociSyncEnvs(rs.Spec.Oci.Image, rs.Spec.Oci.Auth, v1beta1.GetPeriodSecs(rs.Spec.Oci.Period))
	case v1beta1.HelmSource:
		result[reconcilermanager.HelmSync] = helmSyncEnvs(rs.Spec.Helm)
	}
	return result
}

func (r *RootSyncReconciler) validateSpec(ctx context.Context, rs *v1beta1.RootSync, log logr.Logger) error {
	switch v1beta1.SourceType(rs.Spec.SourceType) {
	case v1beta1.GitSource:
		if rs.Spec.Oci != nil {
			return validate.RedundantOciSpec(rs)
		}
		return r.validateGitSpec(ctx, rs, log)
	case v1beta1.OciSource:
		if rs.Spec.Git != nil {
			return validate.RedundantGitSpec(rs)
		}
		return validate.OciSpec(rs.Spec.Oci, rs)
	case v1beta1.HelmSource:
		//TODO: add Helm Source Validation
		return nil
	default:
		return validate.InvalidSourceType(rs)
	}
}

func (r *RootSyncReconciler) validateGitSpec(ctx context.Context, rs *v1beta1.RootSync, log logr.Logger) error {
	if err := validate.GitSpec(rs.Spec.Git, rs); err != nil {
		return err
	}
	return r.validateRootSecret(ctx, rs)
}

func (r *RootSyncReconciler) validateNamespaceName(namespaceName string) error {
	if namespaceName != configsync.ControllerNamespace {
		return fmt.Errorf("RootSync objects are only allowed in the %s namespace, not in %s", configsync.ControllerNamespace, namespaceName)
	}
	return nil
}

// validateRootSecret verify that any necessary Secret is present before creating ConfigMaps and Deployments.
func (r *RootSyncReconciler) validateRootSecret(ctx context.Context, rootSync *v1beta1.RootSync) error {
	if SkipForAuth(rootSync.Spec.Auth) {
		// There is no Secret to check for the Config object.
		return nil
	}
	secret, err := validateSecretExist(ctx,
		rootSync.Spec.SecretRef.Name,
		rootSync.Namespace,
		r.client)
	if err != nil {
		return err
	}
	return validateSecretData(rootSync.Spec.Auth, secret)
}

func (r *RootSyncReconciler) upsertClusterRoleBinding(ctx context.Context, rsNamespace, reconcilerNamespace string) (client.ObjectKey, error) {
	crbRef := client.ObjectKey{Name: RootSyncPermissionsName()}
	childCRB := &rbacv1.ClusterRoleBinding{}
	childCRB.Name = crbRef.Name

	op, err := controllerruntime.CreateOrUpdate(ctx, r.client, childCRB, func() error {
		return r.mutateRootSyncClusterRoleBinding(ctx, childCRB, rsNamespace, reconcilerNamespace)
	})
	if err != nil {
		return crbRef, err
	}
	if op != controllerutil.OperationResultNone {
		r.log.Info("Managed object successfully reconciled",
			logFieldObject, crbRef.String(),
			logFieldKind, "ClusterRoleBinding",
			executedOperation, op)
	}
	return crbRef, nil
}

func (r *RootSyncReconciler) mutateRootSyncClusterRoleBinding(ctx context.Context, crb *rbacv1.ClusterRoleBinding, rsNamespace, reconcilerNamespace string) error {
	crb.OwnerReferences = nil

	// Update rolereference.
	crb.RoleRef = rolereference("cluster-admin", "ClusterRole")

	rsList := &v1beta1.RootSyncList{}
	if err := r.client.List(ctx, rsList, client.InNamespace(rsNamespace)); err != nil {
		return errors.Wrapf(err, "failed to list the RootSync objects")
	}
	return r.updateClusterRoleBindingSubjects(crb, rsList, reconcilerNamespace)
}

func (r *RootSyncReconciler) updateStatus(ctx context.Context, currentRS, rs *v1beta1.RootSync) (bool, error) {
	rs.Status.ObservedGeneration = rs.Generation

	// Avoid unnecessary status updates.
	if cmp.Equal(currentRS.Status, rs.Status, diff.IgnoreTimestampUpdates) {
		klog.V(5).Infof("Skipping status update for RootSync %s/%s", rs.Namespace, rs.Name)
		return false, nil
	}

	if klog.V(5).Enabled() {
		klog.Infof("Updating status for RootSync %s/%s:\nDiff (- Expected, + Actual):\n%s",
			rs.Namespace, rs.Name, cmp.Diff(currentRS.Status, rs.Status))
	}

	err := r.client.Status().Update(ctx, rs)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *RootSyncReconciler) mutationsFor(ctx context.Context, rs *v1beta1.RootSync, containerEnvs map[string][]corev1.EnvVar) mutateFn {
	return func(obj client.Object) error {
		d, ok := obj.(*appsv1.Deployment)
		if !ok {
			return errors.Errorf("expected appsv1 Deployment, got: %T", obj)
		}
		// OwnerReferences, so that when the RootSync CustomResource is deleted,
		// the corresponding Deployment is also deleted.
		d.OwnerReferences = []metav1.OwnerReference{
			ownerReference(
				rs.GroupVersionKind().Kind,
				rs.Name,
				rs.UID,
			),
		}
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
			secretRefName = rs.Spec.SecretRef.Name
			caCertSecretRefName = rs.Spec.Git.CACertSecretRef.Name
		case v1beta1.OciSource:
			auth = rs.Spec.Oci.Auth
			gcpSAEmail = rs.Spec.Oci.GCPServiceAccountEmail
		case v1beta1.HelmSource:
			auth = rs.Spec.Helm.Auth
			gcpSAEmail = rs.Spec.Helm.GCPServiceAccountEmail
			secretRefName = rs.Spec.Helm.SecretRef.Name
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
				mutateContainerResource(ctx, &container, rs.Spec.Override, string(RootReconcilerType))
			case reconcilermanager.HydrationController:
				container.Env = append(container.Env, containerEnvs[container.Name]...)
				if rs.Spec.Override.EnableShellInRendering == nil || !*rs.Spec.Override.EnableShellInRendering {
					container.Image = strings.ReplaceAll(container.Image, reconcilermanager.HydrationControllerWithShell, reconcilermanager.HydrationController)
				} else {
					container.Image = strings.ReplaceAll(container.Image, reconcilermanager.HydrationController+":", reconcilermanager.HydrationControllerWithShell+":")
				}
				mutateContainerResource(ctx, &container, rs.Spec.Override, string(RootReconcilerType))
			case reconcilermanager.OciSync:
				// Don't add the oci-sync container when sourceType is NOT oci.
				if v1beta1.SourceType(rs.Spec.SourceType) != v1beta1.OciSource {
					addContainer = false
				} else {
					container.Env = append(container.Env, containerEnvs[container.Name]...)
					injectFWICredsToContainer(&container, injectFWICreds)
					mutateContainerResource(ctx, &container, rs.Spec.Override, string(RootReconcilerType))
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
					secretName := rs.Spec.SecretRef.Name
					if authTypeToken(rs.Spec.Auth) {
						container.Env = append(container.Env, gitSyncTokenAuthEnv(secretName)...)
					}
					sRef := client.ObjectKey{Namespace: rs.Namespace, Name: secretName}
					keys := GetSecretKeys(ctx, r.client, sRef)
					container.Env = append(container.Env, gitSyncHTTPSProxyEnv(secretName, keys)...)
					mutateContainerResource(ctx, &container, rs.Spec.Override, string(RootReconcilerType))
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
